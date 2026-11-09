// Команда notifications — сервис «Уведомления»: читает события заказов из
// Kafka (топик orders.events, их публикует relay outbox «Заказов»),
// сохраняет уведомление покупателю в SQLite и «отправляет» его (запись в
// журнал). Повторы событий отсеиваются по event_id, необрабатываемые
// записи уходят в DLQ.
//
// Конфигурация — переменные окружения:
//
//	NOTIFICATIONS_KAFKA_BROKERS  адреса брокеров через запятую (обязательно)
//	NOTIFICATIONS_KAFKA_TOPIC    топик событий, по умолчанию orders.events
//	NOTIFICATIONS_KAFKA_GROUP    consumer group, по умолчанию notifications
//	NOTIFICATIONS_DLQ_TOPIC      топик DLQ, по умолчанию notifications.dlq
//	NOTIFICATIONS_MAX_ATTEMPTS   попыток обработки записи, по умолчанию 3
//	NOTIFICATIONS_DB_PATH        файл SQLite, по умолчанию /data/notifications.db
//	NOTIFICATIONS_OPS_ADDR       служебный HTTP (/healthz, /readyz, /metrics), по умолчанию :8081
//	NOTIFICATIONS_OTLP_ENDPOINT  host:port приёмника OTLP/gRPC; пусто — без экспорта
//	NOTIFICATIONS_LOG_LEVEL      debug | info | warn | error, по умолчанию info
//	NOTIFICATIONS_DRAIN_DELAY    пауза между снятием готовности и остановкой, по умолчанию 0
//
// При SIGINT/SIGTERM сервис перестаёт читать, дообрабатывает и коммитит
// текущую пачку, покидает группу и закрывает базу.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"

	"gotutor/labs/notifications/internal/consumer"
	"gotutor/labs/notifications/internal/notify"
	"gotutor/labs/notifications/internal/store/sqlite"
	"gotutor/labs/platform/health"
	"gotutor/labs/platform/httpx"
	"gotutor/labs/platform/kafkax"
	"gotutor/labs/platform/logging"
	"gotutor/labs/platform/telemetry"
)

const (
	serviceName = "notifications"
	// version — версия сборки для ресурса трасс; в настоящем CI её задают
	// через -ldflags "-X main.version=…".
	version = "dev"
)

// main только связывает run с процессом: сигналы, окружение, код выхода.
// Вся логика — в run, её можно вызвать из теста.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "notifications:", err)
		os.Exit(1)
	}
}

const (
	startupTimeout  = 30 * time.Second // база, миграции, создание топика DLQ
	shutdownTimeout = 15 * time.Second // остановка ops-сервера и сброс спанов
	// dlqPartitions — партиций в DLQ. Одна: поток DLQ мал, а глобальный
	// порядок сбоев удобнее для разбора и переотправки вручную.
	dlqPartitions = 1
	// dlqReplicationFactor −1 — фактор репликации по умолчанию брокера
	// (default.replication.factor, KIP-464): в labs брокер один — будет 1,
	// в продакшн-кластере — то, что задали его владельцы (обычно 3).
	dlqReplicationFactor = -1
)

// run запускает сервис и блокируется до отмены ctx или фатальной ошибки.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) (err error) {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: notifications (configured by NOTIFICATIONS_* environment variables)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h: справка уже напечатана
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %q", flags.Args())
	}

	cfg, err := loadConfig(getenv)
	if err != nil {
		return err
	}
	log := logging.New(stdout, cfg.logLevel, serviceName)
	log.InfoContext(ctx, "starting",
		"brokers", cfg.brokers, "topic", cfg.topic, "group", cfg.group,
		"dlq_topic", cfg.dlqTopic, "max_attempts", cfg.maxAttempts, "db_path", cfg.dbPath)

	// Трассировка — первой, закрывается последней: спаны остановки тоже
	// должны уйти в Jaeger.
	tp, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:    serviceName,
		ServiceVersion: version,
		OTLPEndpoint:   cfg.otlpEndpoint,
		SampleRatio:    1, // решение о выборке принимает корень трассы — «Заказы»
		Logger:         log,
	})
	if err != nil {
		return err
	}
	defer func() {
		shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if shErr := tp.Shutdown(shCtx); shErr != nil {
			err = errors.Join(err, fmt.Errorf("tracer shutdown: %w", shErr))
		}
	}()

	// Подключение и миграции — с отдельным таймаутом: недоступная
	// зависимость должна валить старт, а не подвешивать его навсегда.
	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	store, err := sqlite.Open(startCtx, cfg.dbPath)
	if err != nil {
		return err
	}
	// База закрывается после консьюмера (defer выполняются в обратном
	// порядке: клиент Kafka объявлен ниже и закроется раньше).
	defer store.Close()
	if err := store.Migrate(startCtx); err != nil {
		return err
	}
	log.InfoContext(ctx, "migrations applied")

	opts := append(consumer.ClientOptions(cfg.topic, cfg.group), kgo.SeedBrokers(cfg.brokers...))
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return fmt.Errorf("kafka client: %w", err)
	}
	// CloseAllowingRebalance: Run держит пачку под BlockRebalanceOnPoll, а
	// Close без AllowRebalance повис бы на выходе из группы. Выход из
	// группы (LeaveGroup) сразу отдаёт партиции другим репликам, не
	// дожидаясь session timeout.
	defer cl.CloseAllowingRebalance()
	// DLQ принадлежит «Уведомлениям» — сервис создаёт его сам. Топик
	// событий создаёт его владелец, «Заказы».
	if err := kafkax.EnsureTopic(startCtx, cl, cfg.dlqTopic, dlqPartitions, dlqReplicationFactor); err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	ready := &health.Readiness{}
	ready.Add("sqlite", store.Ping)
	ready.Add("kafka", cl.Ping)

	// Ручная сборка зависимостей: весь граф объектов виден в одном месте.
	handler := notify.NewHandler(store, time.Now, log)
	cons := consumer.New(cl, handler, consumer.Config{
		Group:       cfg.group,
		DLQTopic:    cfg.dlqTopic,
		MaxAttempts: cfg.maxAttempts,
	}, log, consumer.NewMetrics(reg))

	opsSrv := httpx.NewServer(httpx.OpsHandler(reg, ready, log), log)
	// Listen отдельно от Serve: занятый порт — ошибка старта, а в журнал
	// попадает настоящий адрес (важно для :0 в тестах).
	ln, err := net.Listen("tcp", cfg.opsAddr)
	if err != nil {
		return fmt.Errorf("listen ops: %w", err)
	}

	// Консьюмер останавливается своим контекстом, а не gctx: сначала
	// снимаем готовность и выжидаем DRAIN_DELAY, потом останавливаем
	// чтение. WithoutCancel сохраняет значения ctx, но не его отмену.
	consCtx, stopConsumer := context.WithCancel(context.WithoutCancel(ctx))
	defer stopConsumer()
	consDone := make(chan struct{})

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.InfoContext(ctx, "ops listening", "addr", ln.Addr().String())
		if err := opsSrv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("ops server: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		defer close(consDone)
		if err := cons.Run(consCtx); err != nil {
			return fmt.Errorf("consumer: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		// Ждём сигнала (отмена ctx) или падения соседа (отмена gctx).
		<-gctx.Done()
		log.InfoContext(ctx, "shutting down")
		// Порядок: готовность снята → пауза, чтобы оркестратор это увидел
		// → консьюмер дообрабатывает и коммитит пачку → ops-сервер
		// (метрики доступны до конца) → клиент Kafka и база (defer выше)
		// → трассировка.
		ready.SetDraining()
		if cfg.drainDelay > 0 {
			t := time.NewTimer(cfg.drainDelay)
			<-t.C
		}
		stopConsumer()
		<-consDone

		shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := opsSrv.Shutdown(shCtx); err != nil {
			return fmt.Errorf("ops shutdown: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}
	log.InfoContext(ctx, "stopped")
	return nil
}

// config — настройки сервиса из окружения.
type config struct {
	brokers      []string
	topic        string
	group        string
	dlqTopic     string
	maxAttempts  int
	dbPath       string
	opsAddr      string
	otlpEndpoint string
	logLevel     slog.Level
	drainDelay   time.Duration
}

// loadConfig читает и проверяет окружение. Ошибка — сразу при старте
// (fail fast), а не на первой записи; все проблемы — разом (errors.Join).
func loadConfig(getenv func(string) string) (config, error) {
	str := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	cfg := config{
		topic:        str("NOTIFICATIONS_KAFKA_TOPIC", "orders.events"),
		group:        str("NOTIFICATIONS_KAFKA_GROUP", "notifications"),
		dlqTopic:     str("NOTIFICATIONS_DLQ_TOPIC", "notifications.dlq"),
		dbPath:       str("NOTIFICATIONS_DB_PATH", "/data/notifications.db"),
		opsAddr:      str("NOTIFICATIONS_OPS_ADDR", ":8081"),
		otlpEndpoint: getenv("NOTIFICATIONS_OTLP_ENDPOINT"),
		maxAttempts:  3,
	}
	var errs []error

	for b := range strings.SplitSeq(getenv("NOTIFICATIONS_KAFKA_BROKERS"), ",") {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(b); err != nil {
			errs = append(errs, fmt.Errorf("NOTIFICATIONS_KAFKA_BROKERS: %q: want host:port", b))
			continue
		}
		cfg.brokers = append(cfg.brokers, b)
	}
	if len(cfg.brokers) == 0 && len(errs) == 0 {
		errs = append(errs, errors.New("NOTIFICATIONS_KAFKA_BROKERS is required (host:port, comma separated)"))
	}
	if cfg.topic == cfg.dlqTopic {
		errs = append(errs, fmt.Errorf("NOTIFICATIONS_DLQ_TOPIC=%q must differ from NOTIFICATIONS_KAFKA_TOPIC", cfg.dlqTopic))
	}
	if v := getenv("NOTIFICATIONS_MAX_ATTEMPTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			errs = append(errs, fmt.Errorf("NOTIFICATIONS_MAX_ATTEMPTS=%q: want an integer from 1 to 100", v))
		}
		cfg.maxAttempts = n
	}
	if _, _, err := net.SplitHostPort(cfg.opsAddr); err != nil {
		errs = append(errs, fmt.Errorf("NOTIFICATIONS_OPS_ADDR=%q: want host:port", cfg.opsAddr))
	}
	if cfg.otlpEndpoint != "" {
		if _, _, err := net.SplitHostPort(cfg.otlpEndpoint); err != nil {
			errs = append(errs, fmt.Errorf("NOTIFICATIONS_OTLP_ENDPOINT=%q: want host:port", cfg.otlpEndpoint))
		}
	}
	level, err := logging.ParseLevel(getenv("NOTIFICATIONS_LOG_LEVEL"))
	if err != nil {
		errs = append(errs, fmt.Errorf("NOTIFICATIONS_LOG_LEVEL: %w", err))
	}
	cfg.logLevel = level
	if v := getenv("NOTIFICATIONS_DRAIN_DELAY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			errs = append(errs, fmt.Errorf("NOTIFICATIONS_DRAIN_DELAY=%q: want a non-negative duration like 5s", v))
		}
		cfg.drainDelay = d
	}
	if err := errors.Join(errs...); err != nil {
		return config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

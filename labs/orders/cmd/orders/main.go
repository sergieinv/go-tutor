// Команда orders — сервис «Заказы»: gRPC OrderService, REST через
// grpc-gateway, сага создания заказа со «Складом», relay outbox → Kafka.
//
// Конфигурация — переменные окружения (см. loadConfig и README). При
// старте сервис применяет миграции и создаёт топик событий, при
// SIGINT/SIGTERM останавливается мягко (см. run).
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
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"buf.build/go/protovalidate"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	ordersv1 "gotutor/labs/api/gen/orders/v1"
	"gotutor/labs/orders/internal/grpcapi"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/outbox"
	"gotutor/labs/orders/internal/store/postgres"
	"gotutor/labs/orders/internal/store/sqlite"
	"gotutor/labs/orders/internal/warehouse"
	"gotutor/labs/platform/grpcx"
	"gotutor/labs/platform/health"
	"gotutor/labs/platform/httpx"
	"gotutor/labs/platform/kafkax"
	"gotutor/labs/platform/logging"
	"gotutor/labs/platform/telemetry"
)

// main только связывает run с процессом: сигналы, окружение, код выхода.
// Вся логика — в run, её можно вызвать из теста.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "orders:", err)
		os.Exit(1)
	}
}

const (
	startupTimeout  = 30 * time.Second // база, миграции, топик
	shutdownTimeout = 20 * time.Second // на доработку начатых запросов
	requestTimeout  = 10 * time.Second // на один REST-запрос
	maxBodyBytes    = 64 << 10         // тело REST-запроса
)

// run запускает сервис и блокируется до отмены ctx или фатальной ошибки.
//
// Вся сборка зависимостей — здесь, явно, в порядке использования: без
// DI-контейнера и Microsoft.Extensions.Hosting граф объектов виден целиком,
// а порядок остановки — обратный порядку создания (defer).
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: orders (configured by ORDERS_* environment variables, see README)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
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
	log := logging.New(stdout, cfg.logLevel, "orders")
	// cfg.dsn — logging.Secret: в журнале будет [REDACTED].
	log.Info("starting", "version", version(), "db", cfg.db, "dsn", cfg.dsn,
		"http_addr", cfg.httpAddr, "grpc_addr", cfg.grpcAddr, "ops_addr", cfg.opsAddr,
		"warehouse", cfg.warehouseAddr, "kafka", cfg.kafkaBrokers, "topic", cfg.kafkaTopic)

	startCtx, cancelStart := context.WithTimeout(ctx, startupTimeout)
	defer cancelStart()

	// Трассировка — первой: TracerProvider закрывается последним (defer)
	// и успевает отправить спаны остановки.
	tp, err := telemetry.Setup(startCtx, telemetry.Config{
		ServiceName: "orders", ServiceVersion: version(),
		OTLPEndpoint: cfg.otlpEndpoint, SampleRatio: 1, Logger: log,
	})
	if err != nil {
		return err
	}
	defer func() {
		shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(shCtx)
	}()

	store, err := openStore(startCtx, cfg)
	if err != nil {
		return err
	}
	defer store.Close() // пул — последним из ресурсов: им пользуются все
	if err := store.Migrate(startCtx); err != nil {
		return err
	}
	log.Info("migrations applied")

	// Kafka: один клиент на процесс. Продюсер franz-go идемпотентный по
	// умолчанию (acks=all + номера последовательностей).
	kafka, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.kafkaBrokers...),
		kgo.ClientID("orders"),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		// Сколько продюсер пытается доставить запись, прежде чем вернуть
		// ошибку relay; дальше повторяет уже relay в следующем такте.
		kgo.RecordDeliveryTimeout(10*time.Second),
	)
	if err != nil {
		return fmt.Errorf("kafka client: %w", err)
	}
	defer kafka.Close()
	if err := kafkax.EnsureTopic(startCtx, kafka, cfg.kafkaTopic, cfg.kafkaPartitions, cfg.kafkaReplication); err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	clientMetrics := grpcx.NewClientMetrics(reg)

	// Соединение со «Складом» — ленивое: недоступный склад не мешает
	// старту, заказы просто не создаются (UNAVAILABLE), а чтение работает.
	whConn, err := grpcx.NewClient(cfg.warehouseAddr, grpcx.ClientConfig{Metrics: clientMetrics})
	if err != nil {
		return fmt.Errorf("warehouse client: %w", err)
	}
	defer whConn.Close()

	svc := orders.NewService(store, warehouse.New(whConn, warehouse.Config{Registerer: reg}), orders.Options{
		Log:      log,
		Observer: newSagaMetrics(reg),
	})

	validator, err := protovalidate.New()
	if err != nil {
		return fmt.Errorf("protovalidate: %w", err)
	}
	grpcSrv := grpcx.NewServer(grpcx.ServerConfig{
		Logger:    log,
		Metrics:   grpcx.NewServerMetrics(reg),
		Validator: validator,
	})
	ordersv1.RegisterOrderServiceServer(grpcSrv, grpcapi.New(svc, log))
	healthSrv := grpchealth.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv)
	if cfg.reflection {
		// Reflection отдаёт описание всех методов любому клиенту
		// (grpcurl list). В labs удобно, в проде — обычно выключено.
		reflection.Register(grpcSrv)
	}

	// Готовность — только база: без неё сервис бесполезен. Ни Kafka (для
	// этого и outbox: заказы принимаются, события уйдут позже), ни склад
	// (без него работают чтение и отмена) в готовность не входят — иначе
	// сбой соседа снимал бы с балансировщика все реплики «Заказов».
	ready := &health.Readiness{}
	ready.Add("db", store.Ping)

	// Listen отдельно от Serve: занятый порт — ошибка старта, а в журнал
	// попадает настоящий адрес (важно для :0 в тестах).
	grpcLn, err := net.Listen("tcp", cfg.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}
	httpLn, err := net.Listen("tcp", cfg.httpAddr)
	if err != nil {
		grpcLn.Close()
		return fmt.Errorf("listen http: %w", err)
	}
	opsLn, err := net.Listen("tcp", cfg.opsAddr)
	if err != nil {
		grpcLn.Close()
		httpLn.Close()
		return fmt.Errorf("listen ops: %w", err)
	}

	// grpc-gateway ходит в собственный gRPC-сервер по сети (loopback), а не
	// вызывает реализацию напрямую (Register…HandlerServer): так REST-запросы
	// проходят те же interceptor'ы — валидацию, журнал, метрики, трассировку.
	gwConn, err := grpcx.NewClient("passthrough:///"+loopback(grpcLn.Addr()), grpcx.ClientConfig{})
	if err != nil {
		return fmt.Errorf("gateway client: %w", err)
	}
	defer gwConn.Close()
	gateway := httpx.NewGatewayMux(log)
	if err := ordersv1.RegisterOrderServiceHandler(ctx, gateway, gwConn); err != nil {
		return fmt.Errorf("register gateway: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/", gateway)
	mux.Handle("GET /healthz", health.LiveHandler())
	mux.Handle("GET /readyz", health.ReadyHandler(ready, log))

	// Цепочка middleware — снаружи внутрь: спан → id запроса → журнал и
	// метрики → recover → дедлайн → лимит тела → маршрутизатор.
	var restHandler http.Handler = http.MaxBytesHandler(mux, maxBodyBytes)
	restHandler = httpx.Timeout(requestTimeout)(restHandler)
	restHandler = httpx.Recover(log)(restHandler)
	restHandler = httpx.Instrument(log, httpx.NewMetrics(reg))(restHandler)
	restHandler = httpx.RequestID(restHandler)
	restHandler = otelhttp.NewHandler(restHandler, "orders.http",
		// Публичная точка входа: traceparent внешнего клиента не делаем
		// родителем (клиент мог бы склеить свои трассы с нашими), а только
		// связываем ссылкой (span link).
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithFilter(func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/v1/") }),
	)
	httpSrv := httpx.NewServer(restHandler, log)
	opsSrv := httpx.NewServer(httpx.OpsHandler(reg, ready, log), log)

	relay := outbox.New(store, kafka, outbox.Config{
		Topic: cfg.kafkaTopic, Interval: cfg.outboxInterval, Logger: log, Registerer: reg,
	})

	cancelStart()

	// relayCtx отменяется отдельно — после остановки серверов: события
	// последних запросов должны успеть уйти.
	relayCtx, stopRelay := context.WithCancel(context.WithoutCancel(ctx))
	defer stopRelay()

	g, gctx := errgroup.WithContext(ctx)
	serveHTTP := func(name string, srv *http.Server, ln net.Listener) {
		g.Go(func() error {
			log.Info("listening", "server", name, "addr", ln.Addr().String())
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("%s server: %w", name, err)
			}
			return nil
		})
	}
	serveHTTP("http", httpSrv, httpLn)
	serveHTTP("ops", opsSrv, opsLn)
	g.Go(func() error {
		log.Info("listening", "server", "grpc", "addr", grpcLn.Addr().String())
		if err := grpcSrv.Serve(grpcLn); err != nil {
			return fmt.Errorf("grpc server: %w", err)
		}
		return nil
	})
	g.Go(func() error { return relay.Run(relayCtx) })
	g.Go(func() error {
		health.SyncGRPC(gctx, ready, healthSrv, 5*time.Second, "", ordersv1.OrderService_ServiceDesc.ServiceName)
		return nil
	})

	g.Go(func() error {
		// Ждём сигнала (отмена ctx) или падения любого сервера (gctx).
		<-gctx.Done()
		log.Info("shutting down")
		shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		// 1. Снять готовность: балансировщик (k8s endpoints) перестаёт слать
		//    новые запросы. Пауза даёт ему это заметить.
		ready.SetDraining()
		healthSrv.Shutdown()
		sleep(shCtx, cfg.drainDelay)

		// 2. Перестать принимать и дождаться начатых: сначала REST (его
		//    запросы идут в gRPC), потом gRPC.
		err := httpSrv.Shutdown(shCtx)
		gracefulStop(shCtx, grpcSrv)

		// 3. Relay публикует оставшееся и завершается.
		stopRelay()

		// 4. Служебный сервер — последним: пробы и метрики видны до конца.
		err = errors.Join(err, opsSrv.Shutdown(shCtx))
		// Клиенты, Kafka и пул закроются в defer после g.Wait.
		return err
	})
	if err := g.Wait(); err != nil {
		return err
	}
	log.Info("stopped")
	return nil
}

// gracefulStop ждёт завершения начатых gRPC-вызовов, но не дольше ctx.
func gracefulStop(ctx context.Context, srv *grpc.Server) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		srv.Stop() // обрывает оставшиеся вызовы
		<-done
	}
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// loopback — адрес для подключения к собственному слушателю: ":9090" и
// "[::]:9090" превращаются в "127.0.0.1:9090".
func loopback(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || tcp.IP.IsUnspecified() {
		_, port, _ := net.SplitHostPort(addr.String())
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr.String()
}

// version — версия сборки из debug.ReadBuildInfo (VCS-ревизия или версия
// модуля). В Docker её можно задать и через -ldflags -X.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			return s.Value[:12]
		}
	}
	return info.Main.Version
}

// config — настройки сервиса из окружения.
type config struct {
	db               string
	dsn              logging.Secret
	httpAddr         string
	grpcAddr         string
	opsAddr          string
	warehouseAddr    string
	kafkaBrokers     []string
	kafkaTopic       string
	kafkaPartitions  int32
	kafkaReplication int16
	otlpEndpoint     string
	logLevel         slog.Level
	reflection       bool
	drainDelay       time.Duration
	outboxInterval   time.Duration
}

// loadConfig читает и проверяет окружение. Ошибка — сразу при старте
// (fail fast), а не на первом запросе; все проблемы сразу (errors.Join).
// В текстах ошибок нет значения DSN.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		db:            getenv("ORDERS_DB"),
		dsn:           logging.Secret(getenv("ORDERS_DSN")),
		httpAddr:      orDefault(getenv("ORDERS_HTTP_ADDR"), ":8080"),
		grpcAddr:      orDefault(getenv("ORDERS_GRPC_ADDR"), ":9090"),
		opsAddr:       orDefault(getenv("ORDERS_OPS_ADDR"), ":8081"),
		warehouseAddr: getenv("ORDERS_WAREHOUSE_ADDR"),
		kafkaTopic:    orDefault(getenv("ORDERS_KAFKA_TOPIC"), "orders.events"),
		otlpEndpoint:  getenv("ORDERS_OTLP_ENDPOINT"),
	}
	var errs []error
	switch cfg.db {
	case "postgres", "sqlite":
	case "":
		errs = append(errs, errors.New("ORDERS_DB is required (postgres or sqlite)"))
	default:
		errs = append(errs, fmt.Errorf("ORDERS_DB=%q: want postgres or sqlite", cfg.db))
	}
	if cfg.dsn == "" {
		errs = append(errs, errors.New("ORDERS_DSN is required"))
	}
	for name, addr := range map[string]string{"ORDERS_HTTP_ADDR": cfg.httpAddr, "ORDERS_GRPC_ADDR": cfg.grpcAddr, "ORDERS_OPS_ADDR": cfg.opsAddr} {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: want host:port", name, addr))
		}
	}
	if cfg.warehouseAddr == "" {
		errs = append(errs, errors.New("ORDERS_WAREHOUSE_ADDR is required (gRPC target, e.g. dns:///warehouse:9090)"))
	}
	for _, b := range strings.Split(getenv("ORDERS_KAFKA_BROKERS"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			cfg.kafkaBrokers = append(cfg.kafkaBrokers, b)
		}
	}
	if len(cfg.kafkaBrokers) == 0 {
		errs = append(errs, errors.New("ORDERS_KAFKA_BROKERS is required (comma-separated host:port)"))
	}
	partitions, err := parseInt(getenv, "ORDERS_KAFKA_PARTITIONS", 3, 1, 1000)
	errs = append(errs, err)
	cfg.kafkaPartitions = int32(partitions)
	replication, err := parseInt(getenv, "ORDERS_KAFKA_REPLICATION", 1, 1, 10)
	errs = append(errs, err)
	cfg.kafkaReplication = int16(replication)

	if cfg.logLevel, err = logging.ParseLevel(getenv("ORDERS_LOG_LEVEL")); err != nil {
		errs = append(errs, fmt.Errorf("ORDERS_LOG_LEVEL: %w", err))
	}
	if v := getenv("ORDERS_GRPC_REFLECTION"); v != "" {
		if cfg.reflection, err = strconv.ParseBool(v); err != nil {
			errs = append(errs, fmt.Errorf("ORDERS_GRPC_REFLECTION=%q: want true or false", v))
		}
	}
	cfg.drainDelay, err = parseDuration(getenv, "ORDERS_DRAIN_DELAY", 0)
	errs = append(errs, err)
	cfg.outboxInterval, err = parseDuration(getenv, "ORDERS_OUTBOX_INTERVAL", 500*time.Millisecond)
	errs = append(errs, err)

	if err := errors.Join(errs...); err != nil {
		return config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseInt(getenv func(string) string, name string, def, lo, hi int) (int, error) {
	v := getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return def, fmt.Errorf("%s=%q: want integer in [%d, %d]", name, v, lo, hi)
	}
	return n, nil
}

func parseDuration(getenv func(string) string, name string, def time.Duration) (time.Duration, error) {
	v := getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return def, fmt.Errorf("%s=%q: want duration like 500ms or 5s", name, v)
	}
	return d, nil
}

// storage — то, что main нужно от хранилища.
type storage interface {
	orders.Store
	outbox.Store
	Migrate(ctx context.Context) error
	Close()
}

// openStore открывает хранилище нужного типа.
func openStore(ctx context.Context, cfg config) (storage, error) {
	switch cfg.db {
	case "postgres":
		return asStorage(postgres.Open(ctx, cfg.dsn.Reveal()))
	case "sqlite":
		return asStorage(sqlite.Open(ctx, cfg.dsn.Reveal()))
	}
	return nil, fmt.Errorf("unknown ORDERS_DB %q", cfg.db) // loadConfig такое не пропустит
}

// asStorage переводит (*T, error) в (storage, error).
//
// Напрямую «return postgres.Open(ctx, dsn)» компилируется, но при ошибке
// вернул бы не nil-интерфейс, а интерфейс с nil-указателем внутри:
// проверка store != nil дала бы true. Здесь при ошибке — настоящий nil.
func asStorage[T storage](s T, err error) (storage, error) {
	if err != nil {
		return nil, err
	}
	return s, nil
}

// sagaMetrics — orders.Observer на Prometheus.
type sagaMetrics struct {
	releases *prometheus.CounterVec
}

func newSagaMetrics(reg prometheus.Registerer) *sagaMetrics {
	m := &sagaMetrics{releases: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_warehouse_releases_total",
		Help: "Reservation releases by reason (reserve_failed and write_failed are saga compensations) and result.",
	}, []string{"reason", "result"})}
	reg.MustRegister(m.releases)
	return m
}

func (m *sagaMetrics) Released(reason string, err error) {
	result := "ok"
	if err != nil {
		result = "failed"
	}
	m.releases.WithLabelValues(reason, result).Inc()
}

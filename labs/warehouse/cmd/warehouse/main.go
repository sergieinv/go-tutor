// Команда warehouse — сервис «Склад»: остатки товара и резервы заказов.
//
// Внутренний API — gRPC WarehouseService (его вызывает «Заказы»); наружу
// через grpc-gateway выставлен только GET /v1/stock/{product_id}. Служебный
// HTTP — /healthz, /readyz, /metrics на отдельном порту.
//
// Конфигурация — переменные окружения:
//
//	WAREHOUSE_MYSQL_DSN         DSN MySQL user:pass@tcp(host:3306)/db (обязательно)
//	WAREHOUSE_HTTP_ADDR         REST (grpc-gateway), по умолчанию :8080
//	WAREHOUSE_GRPC_ADDR         gRPC, по умолчанию :9090
//	WAREHOUSE_OPS_ADDR          служебный HTTP, по умолчанию :8081
//	WAREHOUSE_OTLP_ENDPOINT     host:port приёмника OTLP/gRPC; пусто — без экспорта
//	WAREHOUSE_LOG_LEVEL         debug | info | warn | error, по умолчанию info
//	WAREHOUSE_DRAIN_DELAY       пауза между снятием готовности и остановкой, по умолчанию 0
//	WAREHOUSE_GRPC_REFLECTION   true — включить gRPC reflection (grpcurl без .proto)
//
// При старте сервис применяет миграции; по SIGINT/SIGTERM снимает
// готовность, дорабатывает начатые вызовы и закрывает пул базы.
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
	"syscall"
	"time"

	"buf.build/go/protovalidate"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/platform/grpcx"
	"gotutor/labs/platform/health"
	"gotutor/labs/platform/httpx"
	"gotutor/labs/platform/logging"
	"gotutor/labs/platform/telemetry"
	"gotutor/labs/warehouse/internal/grpcapi"
	"gotutor/labs/warehouse/internal/stock"
	"gotutor/labs/warehouse/internal/store/mysql"
)

// version — версия сборки (go build -ldflags "-X main.version=…").
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "warehouse:", err)
		os.Exit(1)
	}
}

const (
	startupTimeout     = 30 * time.Second // подключение к базе и миграции
	shutdownTimeout    = 15 * time.Second // на доработку начатых вызовов
	httpRequestTimeout = 5 * time.Second  // дедлайн обработки REST-запроса
	healthSyncInterval = 5 * time.Second  // готовность → gRPC health
	maxBodyBytes       = 64 << 10         // тело REST-запроса
)

// run запускает сервис и блокируется до отмены ctx или фатальной ошибки.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: warehouse (configured by WAREHOUSE_* environment variables, see README)")
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
	log := logging.New(stdout, cfg.logLevel, "warehouse")
	// cfg.dsn — logging.Secret: даже случайно попав в журнал, он выйдет
	// как [REDACTED].
	log.InfoContext(ctx, "starting", "version", version, "http_addr", cfg.httpAddr,
		"grpc_addr", cfg.grpcAddr, "ops_addr", cfg.opsAddr, "otlp_endpoint", cfg.otlpEndpoint)

	// Трассировка — первой: провайдер закрывается последним (defer
	// выполняются в обратном порядке) и успевает отправить спаны остановки.
	tp, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:    "warehouse",
		ServiceVersion: version,
		OTLPEndpoint:   cfg.otlpEndpoint,
		SampleRatio:    1, // учебный стенд: пишем все трассы
		Logger:         log,
	})
	if err != nil {
		return err
	}
	defer func() {
		shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(shCtx); err != nil {
			log.WarnContext(shCtx, "tracer shutdown", "err", err)
		}
	}()

	// Метрики — свой реестр, а не глобальный prometheus.DefaultRegisterer:
	// видно, что именно сервис публикует, а тесты не делят состояние.
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// База и миграции — с отдельным таймаутом: недоступная база валит
	// старт, а не подвешивает его.
	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	store, err := mysql.Open(startCtx, cfg.dsn.Reveal(), reg)
	if err != nil {
		return err
	}
	defer store.Close() // пул закрывается после остановки серверов
	if err := store.Migrate(startCtx); err != nil {
		return err
	}
	log.InfoContext(ctx, "migrations applied")

	// Ручная сборка зависимостей: весь граф объектов виден здесь.
	svc := stock.NewService(store, time.Now, stock.NewMetrics(reg))
	readiness := &health.Readiness{}
	readiness.Add("mysql", store.Ping)

	// gRPC-сервер: interceptor'ы, лимиты и keepalive — из platform/grpcx.
	validator, err := protovalidate.New()
	if err != nil {
		return fmt.Errorf("protovalidate: %w", err)
	}
	grpcSrv := grpcx.NewServer(grpcx.ServerConfig{
		Logger:    log,
		Metrics:   grpcx.NewServerMetrics(reg),
		Validator: validator,
	})
	warehousev1.RegisterWarehouseServiceServer(grpcSrv, grpcapi.New(svc, log))
	healthSrv := grpchealth.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, healthSrv)
	if cfg.reflection {
		// Reflection раскрывает весь контракт любому клиенту — удобно для
		// grpcurl на стенде, но по умолчанию выключено.
		reflection.Register(grpcSrv)
	}

	// Listen отдельно от Serve: занятый порт — ошибка старта, а в журнал
	// попадает настоящий адрес (важно для :0 в тестах).
	grpcLn, err := net.Listen("tcp", cfg.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}
	defer grpcLn.Close()
	httpLn, err := net.Listen("tcp", cfg.httpAddr)
	if err != nil {
		return fmt.Errorf("listen http: %w", err)
	}
	defer httpLn.Close()
	opsLn, err := net.Listen("tcp", cfg.opsAddr)
	if err != nil {
		return fmt.Errorf("listen ops: %w", err)
	}
	defer opsLn.Close()

	// grpc-gateway ходит в собственный gRPC-сервер по сети, а не вызывает
	// обработчики напрямую (RegisterWarehouseServiceHandlerServer): так
	// REST-запрос проходит те же interceptor'ы — валидацию, журнал,
	// метрики, дедлайн — что и gRPC-вызов.
	conn, err := grpcx.NewClient("passthrough:///"+dialAddr(grpcLn.Addr()), grpcx.ClientConfig{})
	if err != nil {
		return fmt.Errorf("gateway client: %w", err)
	}
	defer conn.Close()
	gw := httpx.NewGatewayMux(log)
	if err := warehousev1.RegisterWarehouseServiceHandler(ctx, gw, conn); err != nil {
		return fmt.Errorf("register gateway: %w", err)
	}
	// Порядок middleware: otelhttp открывает спан первым (trace_id есть во
	// всех журналах запроса), затем id запроса, журнал с метриками, Recover
	// (его 500 видят журнал и метрики), дедлайн обработки и лимит тела
	// (сейчас REST — только GET, но маршрут с телом не должен остаться без него).
	var api http.Handler = http.MaxBytesHandler(gw, maxBodyBytes)
	api = httpx.Timeout(httpRequestTimeout)(api)
	api = httpx.Recover(log)(api)
	api = httpx.Instrument(log, httpx.NewMetrics(reg))(api)
	api = httpx.RequestID(api)
	api = otelhttp.NewHandler(api, "warehouse-http")
	httpSrv := httpx.NewServer(api, log)
	opsSrv := httpx.NewServer(httpx.OpsHandler(reg, readiness, log), log)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.InfoContext(ctx, "listening", "server", "grpc", "addr", grpcLn.Addr().String())
		if err := grpcSrv.Serve(grpcLn); err != nil {
			return fmt.Errorf("grpc server: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		log.InfoContext(ctx, "listening", "server", "http", "addr", httpLn.Addr().String())
		if err := httpSrv.Serve(httpLn); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		log.InfoContext(ctx, "listening", "server", "ops", "addr", opsLn.Addr().String())
		if err := opsSrv.Serve(opsLn); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("ops server: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		health.SyncGRPC(gctx, readiness, healthSrv, healthSyncInterval, "", warehousev1.WarehouseService_ServiceDesc.ServiceName)
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		return shutdown(ctx, log, cfg.drainDelay, readiness, healthSrv, grpcSrv, httpSrv, opsSrv)
	})
	if err := g.Wait(); err != nil {
		return err
	}
	log.InfoContext(ctx, "stopped")
	return nil
}

// shutdown останавливает сервис по шагам. Пул базы и провайдер трассировки
// закрывают defer в run — после того, как здесь всё остановилось.
func shutdown(ctx context.Context, log *slog.Logger, drainDelay time.Duration, readiness *health.Readiness,
	healthSrv *grpchealth.Server, grpcSrv *grpc.Server, httpSrv, opsSrv *http.Server,
) error {
	// ctx уже отменён; остановке нужен свой срок. WithoutCancel сохраняет
	// значения ctx, но не отмену.
	shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout+drainDelay)
	defer cancel()
	log.InfoContext(shCtx, "shutting down", "drain_delay", drainDelay.String())

	// 1. Снять готовность: /readyz → 503, gRPC health → NOT_SERVING.
	// Балансировщик (k8s Endpoints) убирает реплику не мгновенно, поэтому
	// дальше — пауза, в течение которой трафик ещё обслуживается.
	readiness.SetDraining()
	healthSrv.Shutdown()
	if drainDelay > 0 {
		t := time.NewTimer(drainDelay)
		select {
		case <-t.C:
		case <-shCtx.Done():
			t.Stop()
		}
	}

	// 2. Перестать принимать и дождаться начатых: REST (он ходит в gRPC,
	// поэтому раньше gRPC), затем gRPC.
	var errs []error
	if err := httpSrv.Shutdown(shCtx); err != nil {
		errs = append(errs, fmt.Errorf("http shutdown: %w", err))
	}
	stopped := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-shCtx.Done():
		// Срок вышел: обрываем оставшиеся вызовы. GracefulStop выше
		// после этого тоже вернётся.
		grpcSrv.Stop()
		<-stopped
		errs = append(errs, errors.New("grpc graceful stop timed out"))
	}

	// 3. Служебный HTTP — последним: пробы и /metrics нужны до конца.
	if err := opsSrv.Shutdown(shCtx); err != nil {
		errs = append(errs, fmt.Errorf("ops shutdown: %w", err))
	}
	return errors.Join(errs...)
}

// dialAddr — адрес, по которому процесс достучится до своего слушателя.
// ":9090" слушает все интерфейсы ([::]:9090), а подключаться надо к
// конкретному — к loopback.
func dialAddr(a net.Addr) string {
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return a.String()
	}
	if tcp.IP == nil || tcp.IP.IsUnspecified() {
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(tcp.Port))
	}
	return tcp.String()
}

// config — настройки сервиса из окружения.
type config struct {
	dsn          logging.Secret
	httpAddr     string
	grpcAddr     string
	opsAddr      string
	otlpEndpoint string
	logLevel     slog.Level
	drainDelay   time.Duration
	reflection   bool
}

// loadConfig читает и проверяет окружение. Все проблемы — сразу и вместе
// (errors.Join), при старте, а не на первом запросе. Значение DSN в текст
// ошибок не попадает.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		dsn:          logging.Secret(getenv("WAREHOUSE_MYSQL_DSN")),
		httpAddr:     withDefault(getenv("WAREHOUSE_HTTP_ADDR"), ":8080"),
		grpcAddr:     withDefault(getenv("WAREHOUSE_GRPC_ADDR"), ":9090"),
		opsAddr:      withDefault(getenv("WAREHOUSE_OPS_ADDR"), ":8081"),
		otlpEndpoint: getenv("WAREHOUSE_OTLP_ENDPOINT"),
	}
	var errs []error
	if cfg.dsn == "" {
		errs = append(errs, errors.New("WAREHOUSE_MYSQL_DSN is required"))
	}
	for _, a := range []struct{ name, value string }{
		{"WAREHOUSE_HTTP_ADDR", cfg.httpAddr},
		{"WAREHOUSE_GRPC_ADDR", cfg.grpcAddr},
		{"WAREHOUSE_OPS_ADDR", cfg.opsAddr},
	} {
		if _, _, err := net.SplitHostPort(a.value); err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: want host:port", a.name, a.value))
		}
	}
	if cfg.otlpEndpoint != "" {
		if _, _, err := net.SplitHostPort(cfg.otlpEndpoint); err != nil {
			errs = append(errs, fmt.Errorf("WAREHOUSE_OTLP_ENDPOINT=%q: want host:port", cfg.otlpEndpoint))
		}
	}
	if v := getenv("WAREHOUSE_LOG_LEVEL"); v != "" {
		level, err := logging.ParseLevel(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("WAREHOUSE_LOG_LEVEL=%q: want debug, info, warn or error", v))
		}
		cfg.logLevel = level
	}
	if v := getenv("WAREHOUSE_DRAIN_DELAY"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			errs = append(errs, fmt.Errorf("WAREHOUSE_DRAIN_DELAY=%q: want a non-negative duration like 5s", v))
		}
		cfg.drainDelay = d
	}
	if v := getenv("WAREHOUSE_GRPC_REFLECTION"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("WAREHOUSE_GRPC_REFLECTION=%q: want true or false", v))
		}
		cfg.reflection = b
	}
	if err := errors.Join(errs...); err != nil {
		return config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

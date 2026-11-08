// Команда orders — HTTP-сервис «Заказы».
//
// Конфигурация — переменные окружения:
//
//	ORDERS_DB    postgres | mysql | sqlite (обязательно)
//	ORDERS_DSN   строка подключения; для sqlite — путь к файлу (обязательно)
//	ORDERS_ADDR  адрес HTTP-сервера, по умолчанию :8080
//
// При старте сервис применяет миграции, при SIGINT/SIGTERM завершается
// мягко: дорабатывает начатые запросы и закрывает соединения с базой.
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
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"gotutor/labs/orders/internal/httpapi"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/mysql"
	"gotutor/labs/orders/internal/store/postgres"
	"gotutor/labs/orders/internal/store/sqlite"
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
	startupTimeout  = 30 * time.Second // подключение к базе и миграции
	shutdownTimeout = 15 * time.Second // на доработку начатых запросов
)

// run запускает сервис и блокируется до отмены ctx или фатальной ошибки.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: orders (configured by ORDERS_DB, ORDERS_DSN, ORDERS_ADDR)")
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
	log := slog.New(slog.NewJSONHandler(stdout, nil))
	// DSN не журналируем: в нём пароль.
	log.Info("starting", "db", cfg.db, "addr", cfg.addr)

	// Подключение и миграции — с отдельным таймаутом: недоступная база
	// должна валить старт, а не подвешивать его навсегда.
	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	store, err := openStore(startCtx, cfg)
	if err != nil {
		return err
	}
	// Пул закрывается последним — после HTTP-сервера и фоновых воркеров,
	// которые им пользуются.
	defer store.Close()

	if err := store.Migrate(startCtx); err != nil {
		return err
	}
	log.Info("migrations applied")

	// Ручная сборка зависимостей: весь граф объектов виден в одном месте.
	svc := orders.NewService(store, time.Now)
	srv := &http.Server{
		Handler: httpapi.New(svc, store.Ping, log),
		// Таймауты обязательны: без них медленный клиент держит соединение
		// и горутину сколько угодно (slowloris).
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Listen отдельно от Serve: занятый порт — ошибка старта, а в журнал
	// попадает настоящий адрес (важно для ORDERS_ADDR=:0 в тестах).
	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		log.Info("listening", "addr", ln.Addr().String())
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})
	// Здесь в модуле 12 появится публикатор outbox: g.Go(publisher.Run).
	g.Go(func() error {
		// Ждём сигнала (отмена ctx) или падения сервера (отмена gctx).
		<-gctx.Done()
		log.Info("shutting down")
		// Shutdown получает свой контекст: ctx уже отменён, а начатым
		// запросам нужно дать время доработать. WithoutCancel сохраняет
		// значения ctx, но не его отмену.
		shCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		// Порядок: сначала HTTP перестаёт принимать запросы и дожидается
		// начатых, затем останавливаются фоновые воркеры, последним
		// закрывается пул (defer store.Close выше).
		if err := srv.Shutdown(shCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}
	log.Info("stopped")
	return nil
}

// config — настройки сервиса из окружения.
type config struct {
	db   string
	dsn  string
	addr string
}

// loadConfig читает и проверяет окружение. Ошибка — сразу при старте
// (fail fast), а не на первом запросе. В текстах ошибок нет значения DSN.
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		db:   getenv("ORDERS_DB"),
		dsn:  getenv("ORDERS_DSN"),
		addr: getenv("ORDERS_ADDR"),
	}
	if cfg.addr == "" {
		cfg.addr = ":8080"
	}
	var errs []error
	switch cfg.db {
	case "postgres", "mysql", "sqlite":
	case "":
		errs = append(errs, errors.New("ORDERS_DB is required (postgres, mysql or sqlite)"))
	default:
		errs = append(errs, fmt.Errorf("ORDERS_DB=%q: want postgres, mysql or sqlite", cfg.db))
	}
	if cfg.dsn == "" {
		errs = append(errs, errors.New("ORDERS_DSN is required"))
	}
	if _, _, err := net.SplitHostPort(cfg.addr); err != nil {
		errs = append(errs, fmt.Errorf("ORDERS_ADDR=%q: want host:port", cfg.addr))
	}
	// errors.Join: все проблемы конфигурации сразу, а не по одной за запуск.
	if err := errors.Join(errs...); err != nil {
		return config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// storage — то, что main нужно от хранилища сверх orders.Store.
type storage interface {
	orders.Store
	Migrate(ctx context.Context) error
	Close()
}

// openStore открывает хранилище нужного типа.
func openStore(ctx context.Context, cfg config) (storage, error) {
	switch cfg.db {
	case "postgres":
		return asStorage(postgres.Open(ctx, cfg.dsn))
	case "mysql":
		return asStorage(mysql.Open(ctx, cfg.dsn))
	case "sqlite":
		return asStorage(sqlite.Open(ctx, cfg.dsn))
	}
	return nil, fmt.Errorf("unknown ORDERS_DB %q", cfg.db) // loadConfig такое не пропустит
}

// asStorage переводит (*T, error) в (storage, error).
//
// Напрямую «return postgres.Open(ctx, cfg.dsn)» компилируется, но при
// ошибке вернул бы не nil-интерфейс, а интерфейс с nil-указателем
// *postgres.Store внутри: проверка store != nil дала бы true. Здесь при
// ошибке возвращается настоящий nil.
func asStorage[T storage](s T, err error) (storage, error) {
	if err != nil {
		return nil, err
	}
	return s, nil
}

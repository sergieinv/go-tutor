// В проекте это `cmd/orders/main.go`, а хранилище, сервис и
// HTTP — пакеты в `internal/`. В песочнице файлы плоские,
// поэтому здесь всё в одном файле, а «пакеты» — блоки ниже.

package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// Секрет печатается как `***` — даже в `%+v` всей
// конфигурации. Работает только для экспортируемых полей:
// у неэкспортируемых `fmt` метод `String` не вызывает.
type secret string

func (secret) String() string { return "***" }

// Вся конфигурация — одна структура. Значения по умолчанию,
// поверх них окружение, поверх него флаги.
type config struct {
	Addr     string
	DBPath   string
	Shutdown time.Duration
	APIToken secret
}

func loadConfig(args []string, getenv func(string) string) (config, error) {
	cfg := config{Addr: "127.0.0.1:8080", DBPath: "orders.db", Shutdown: 10 * time.Second}
	if v := getenv("ORDERS_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("ORDERS_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	cfg.APIToken = secret(getenv("ORDERS_API_TOKEN"))
	fs := flag.NewFlagSet("orders", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "адрес HTTP")
	fs.DurationVar(&cfg.Shutdown, "shutdown-timeout", cfg.Shutdown, "лимит остановки")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	// Проверяем всё сразу и сообщаем обо всех ошибках, а не
	// по одной за перезапуск.
	var errs []error
	if cfg.Shutdown <= 0 {
		errs = append(errs, errors.New("-shutdown-timeout должен быть > 0"))
	}
	if cfg.APIToken == "" {
		errs = append(errs, errors.New("ORDERS_API_TOKEN не задан"))
	}
	return cfg, errors.Join(errs...)
}

// «internal/orders»: доменный пакет. Интерфейс хранилища
// объявлен здесь — у потребителя, и в нём только то, что
// сервису нужно.
type orderStore interface {
	Count(ctx context.Context) (int, error)
}

type orderService struct{ store orderStore }

func (s *orderService) Stats(ctx context.Context) (string, error) {
	n, err := s.store.Count(ctx)
	if err != nil {
		return "", fmt.Errorf("stats: %w", err)
	}
	return fmt.Sprintf("заказов: %d", n), nil
}

// «internal/orders/sqlite»: реализация возвращает конкретный
// тип и ничего не знает об интерфейсе выше.
type sqliteStore struct{ db *sql.DB }

func (s *sqliteStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM orders`).Scan(&n)
	return n, err
}

// `run` — весь `main`, но тестируемый: аргументы, окружение и
// вывод приходят параметрами, а вместо `os.Exit` — ошибка.
// Зависимости собираются вручную, в порядке, который видно.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	cfg, err := loadConfig(args, getenv)
	if err != nil {
		return fmt.Errorf("конфигурация: %w", err) // fail fast: ещё ничего не открыто
	}
	fmt.Fprintf(stdout, "конфигурация: %+v\n", cfg)

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close() // закроется последним — после сервера
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS orders (id INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("база: %w", err)
	}

	svc := &orderService{store: &sqliteStore{db: db}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		s, err := svc.Stats(r.Context())
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, s)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	// Проверяем себя запросом, как это сделал бы балансировщик.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/stats")
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Fprintf(stdout, "GET /stats: %d %s", resp.StatusCode, body)
	syscall.Kill(os.Getpid(), syscall.SIGTERM) // только в примере: «оркестратор» останавливает нас

	<-ctx.Done()
	fmt.Fprintln(stdout, "сигнал получен, останавливаемся")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Shutdown)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Настоящий `main` — несколько строк: контекст от сигналов,
// `run(ctx, os.Args[1:], os.Getenv, os.Stdout)` и при ошибке
// `os.Exit(1)`. Здесь `run` вызван так, как вызвал бы тест:
// с картой вместо окружения.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dir, _ := os.MkdirTemp("", "orders")
	defer os.RemoveAll(dir)
	os.Chdir(dir) // база orders.db — во временном каталоге

	env := map[string]string{"ORDERS_API_TOKEN": "s3cr3t-token"}
	err := run(ctx, []string{"-addr", "127.0.0.1:0"}, func(k string) string { return env[k] }, os.Stdout)
	fmt.Println("run:", err)

	err = run(ctx, []string{"-shutdown-timeout", "0s"}, func(string) string { return "" }, os.Stdout)
	fmt.Println("run:", err)
}

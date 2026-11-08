// Склад из трёх слоёв — хранилище, сервис, HTTP — и путь
// ошибки и контекста через них. В конце — остановка по
// сигналу в правильном порядке.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	_ "modernc.org/sqlite"
)

// Доменные ошибки объявляет сервис: хендлер знает их, но
// не знает ни `sql.ErrNoRows`, ни кодов SQLite.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type ValidationError struct{ Field, Msg string }

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

// Хранилище переводит ошибки базы в доменные и передаёт
// `ctx` в каждый запрос: дедлайн запроса прерывает SQL.
type store struct{ db *sql.DB }

func (s *store) reserve(ctx context.Context, id, qty int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE items SET stock = stock - ? WHERE id = ? AND stock >= ?`, qty, id, qty)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var one int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return ErrConflict
}

// Отчёт «считается долго»: бесконечный рекурсивный запрос,
// который остановит только отмена контекста.
func (s *store) report(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c) SELECT count(*) FROM c`).Scan(&n)
	return n, err
}

// Сервис проверяет ввод и оборачивает ошибки контекстом
// операции, сохраняя причину через `%w`.
type service struct{ st *store }

func (s *service) Reserve(ctx context.Context, id, qty int) error {
	if qty <= 0 {
		return &ValidationError{Field: "qty", Msg: "must be positive"}
	}
	if err := s.st.reserve(ctx, id, qty); err != nil {
		return fmt.Errorf("reserve item %d: %w", id, err)
	}
	return nil
}

// Единственное место, где ошибка становится HTTP-статусом.
// Подробности — в лог, клиенту — общий текст.
func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		http.Error(w, ve.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrConflict):
		http.Error(w, "not enough stock", http.StatusConflict)
	case errors.Is(err, context.DeadlineExceeded):
		log.Warn("timeout", "path", r.URL.Path, "err", err)
		http.Error(w, "timeout", http.StatusServiceUnavailable)
	default:
		log.Error("request failed", "path", r.URL.Path, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func newHandler(svc *service, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /items/{id}/reserve", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			writeError(w, r, log, &ValidationError{Field: "id", Msg: "must be integer"})
			return
		}
		qty, _ := strconv.Atoi(r.URL.Query().Get("qty")) // не число → 0 → ошибка проверки
		if err := svc.Reserve(r.Context(), id, qty); err != nil {
			writeError(w, r, log, err)
			return
		}
		fmt.Fprintln(w, "reserved")
	})
	// Свой дедлайн на операцию — дочерний к контексту запроса:
	// отмена клиентом тоже дойдёт до SQL.
	mux.HandleFunc("GET /report", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 50*time.Millisecond)
		defer cancel()
		n, err := svc.st.report(ctx)
		if err != nil {
			writeError(w, r, log, err)
			return
		}
		fmt.Fprintln(w, n)
	})
	mux.HandleFunc("GET /broken", func(w http.ResponseWriter, r *http.Request) {
		_, err := svc.st.db.ExecContext(r.Context(), `DELETE FROM no_such_table`)
		writeError(w, r, log, fmt.Errorf("cleanup: %w", err))
	})
	return mux
}

func main() {
	// Логгер без времени — для стабильного вывода.
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Ошибка при старте — выход с кодом 1 (fail fast).
	check := func(err error) {
		if err != nil {
			log.Error("startup", "err", err)
			os.Exit(1)
		}
	}
	dir, err := os.MkdirTemp("", "stock")
	check(err)
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "stock.db"))
	check(err)
	_, err = db.ExecContext(ctx, `CREATE TABLE items (id INTEGER PRIMARY KEY, stock INTEGER NOT NULL);
		INSERT INTO items VALUES (1, 5)`)
	check(err)

	srv := &http.Server{Handler: newHandler(&service{st: &store{db: db}}, log), ReadHeaderTimeout: time.Second}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	check(err)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	// Фоновый воркер живёт в своей группе со своим контекстом:
	// его остановят после HTTP, а не одновременно.
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	workers, wctx := errgroup.WithContext(workerCtx)
	workers.Go(func() error {
		t := time.NewTicker(10 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-wctx.Done():
				fmt.Println("   воркер: вышел по отмене контекста")
				return nil
			case <-t.C:
				// в сервисе: отправка outbox, чистка просроченных резервов
			}
		}
	})

	client := &http.Client{Timeout: 5 * time.Second}
	for _, req := range []string{"POST /items/1/reserve?qty=2", "POST /items/1/reserve?qty=0",
		"POST /items/9/reserve?qty=1", "POST /items/x/reserve?qty=1", "POST /items/1/reserve?qty=10", "GET /report", "GET /broken"} {
		method, path, _ := strings.Cut(req, " ")
		r, _ := http.NewRequest(method, "http://"+ln.Addr().String()+path, nil)
		resp, err := client.Do(r)
		if err != nil {
			fmt.Println(err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%-28s → %d %s", req, resp.StatusCode, body)
	}

	// Оркестратор присылает SIGTERM (здесь — мы сами).
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	<-ctx.Done()
	fmt.Println("1. сигнал: перестаём принимать соединения")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shCtx); err != nil {
		fmt.Println("shutdown:", err)
	}
	<-serveErr // ErrServerClosed — штатный исход
	fmt.Println("2. HTTP: активные запросы завершены")
	stopWorkers()
	if err := workers.Wait(); err != nil {
		fmt.Println("воркеры:", err)
	}
	fmt.Println("3. воркеры остановлены")
	db.Close()
	fmt.Println("4. база закрыта — она нужна была до последнего запроса")
}

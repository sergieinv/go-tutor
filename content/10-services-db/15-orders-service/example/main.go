// Сервис «Заказы» целиком, в одном файле на SQLite. Разделы
// файла повторяют пакеты полноценного сервиса `labs/orders`:
// домен (`internal/orders`), хранилище (`internal/store/sqlite`),
// HTTP (`internal/httpapi`) и сборка в `main` (`cmd/orders`).

package main

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// Домен не знает ни про HTTP, ни про SQL. Доменные ошибки —
// значения для `errors.Is`, ошибка валидации — тип с полем,
// которое безопасно показать клиенту.
type Status string

const (
	StatusReserved  Status = "reserved"
	StatusCancelled Status = "cancelled"
)

type Item struct {
	ProductID int64
	Quantity  int
}

type Order struct {
	ID     int64
	Status Status
	Items  []Item
}

var (
	ErrNotFound   = errors.New("not found")
	ErrOutOfStock = errors.New("out of stock")
	ErrConflict   = errors.New("conflict")
)

type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string { return "invalid " + e.Field + ": " + e.Message }

// Интерфейс хранилища объявляет потребитель — сервис. Сервис
// проверяет правила, хранилище отвечает за атомарность.
type Store interface {
	CreateOrder(ctx context.Context, items []Item) (Order, error)
	Order(ctx context.Context, id int64) (Order, error)
	CancelOrder(ctx context.Context, id int64) (Order, error)
}

type Service struct{ store Store }

func (s *Service) Create(ctx context.Context, items []Item) (Order, error) {
	if len(items) == 0 || len(items) > 50 {
		return Order{}, &ValidationError{"items", "must contain 1..50 items"}
	}
	seen := make(map[int64]bool, len(items))
	for i, it := range items {
		if it.Quantity < 1 || it.Quantity > 1000 {
			return Order{}, &ValidationError{fmt.Sprintf("items[%d].quantity", i), "must be 1..1000"}
		}
		if seen[it.ProductID] {
			return Order{}, &ValidationError{fmt.Sprintf("items[%d].product_id", i), "duplicate product"}
		}
		seen[it.ProductID] = true
	}
	return s.store.CreateOrder(ctx, items)
}

// Хранилище. Транзакция — явная: `withTx` откатывает её при
// любой ошибке (и панике), фиксирует при успехе.
type SQLiteStore struct{ db *sql.DB }

func withTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Заказ, резерв и событие outbox — одна транзакция: либо всё,
// либо ничего. Резерв — атомарный `UPDATE … WHERE available >= ?`,
// без «прочитал остаток — записал». Позиции идут по возрастанию
// id товара: встречные заказы блокируют строки в одном порядке
// и не ловят дедлок в PostgreSQL и MySQL.
func (s *SQLiteStore) CreateOrder(ctx context.Context, items []Item) (Order, error) {
	items = slices.SortedFunc(slices.Values(items), func(a, b Item) int { return cmp.Compare(a.ProductID, b.ProductID) })
	o := Order{Status: StatusReserved, Items: items}
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO orders (status) VALUES (?)`, o.Status)
		if err != nil {
			return fmt.Errorf("insert order: %w", err)
		}
		if o.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		for i, it := range items {
			res, err := tx.ExecContext(ctx, `UPDATE products SET available = available - ?
				WHERE id = ? AND available >= ?`, it.Quantity, it.ProductID, it.Quantity)
			if err != nil {
				return fmt.Errorf("reserve product %d: %w", it.ProductID, err)
			}
			if n, err := res.RowsAffected(); err != nil || n == 0 {
				return s.whyNotReserved(ctx, tx, i, it.ProductID, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO order_items (order_id, product_id, quantity) VALUES (?, ?, ?)`,
				o.ID, it.ProductID, it.Quantity); err != nil {
				return fmt.Errorf("insert item: %w", err)
			}
		}
		return addEvent(ctx, tx, "order.created", o)
	})
	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}
	return o, nil
}

// whyNotReserved отличает «нет товара» от «не хватает».
func (s *SQLiteStore) whyNotReserved(ctx context.Context, tx *sql.Tx, i int, id int64, err error) error {
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM products WHERE id = ?`, id).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return &ValidationError{fmt.Sprintf("items[%d].product_id", i), "unknown product"}
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("product %d: %w", id, ErrOutOfStock)
}

func addEvent(ctx context.Context, tx *sql.Tx, typ string, o Order) error {
	payload, err := json.Marshal(map[string]any{"order_id": o.ID, "status": o.Status})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox (type, payload) VALUES (?, ?)`, typ, payload)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// Заказ с позициями — два запроса, а не 1 + N.
// `defer rows.Close()` и `rows.Err()` — обязательная пара.
func (s *SQLiteStore) Order(ctx context.Context, id int64) (Order, error) {
	return order(ctx, s.db, id)
}

type querier interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

func order(ctx context.Context, q querier, id int64) (Order, error) {
	o := Order{ID: id}
	err := q.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, id).Scan(&o.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, fmt.Errorf("order %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Order{}, fmt.Errorf("select order %d: %w", id, err)
	}
	rows, err := q.QueryContext(ctx, `SELECT product_id, quantity FROM order_items WHERE order_id = ? ORDER BY product_id`, id)
	if err != nil {
		return Order{}, fmt.Errorf("select items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ProductID, &it.Quantity); err != nil {
			return Order{}, err
		}
		o.Items = append(o.Items, it)
	}
	return o, rows.Err()
}

// Смена статуса — условный `UPDATE`: параллельная отмена не
// пройдёт дважды. Остаток возвращается в той же транзакции.
func (s *SQLiteStore) CancelOrder(ctx context.Context, id int64) (Order, error) {
	var o Order
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE orders SET status = ? WHERE id = ? AND status = ?`,
			StatusCancelled, id, StatusReserved)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			if _, err := order(ctx, tx, id); err != nil {
				return err // ErrNotFound
			}
			return fmt.Errorf("order %d: %w", id, ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE products SET available = available +
			(SELECT quantity FROM order_items WHERE order_id = ? AND product_id = products.id)
			WHERE id IN (SELECT product_id FROM order_items WHERE order_id = ?)`, id, id); err != nil {
			return fmt.Errorf("release stock: %w", err)
		}
		if o, err = order(ctx, tx, id); err != nil {
			return err
		}
		return addEvent(ctx, tx, "order.cancelled", o)
	})
	if err != nil {
		return Order{}, fmt.Errorf("cancel order: %w", err)
	}
	return o, nil
}

// HTTP — тонкий слой: разобрать запрос с лимитом тела, вызвать
// сервис, перевести ошибку в статус. Контекст запроса
// (`r.Context()`) идёт до самого SQL.
type handler struct {
	svc   *Service
	store Store
	log   *slog.Logger
}

type itemJSON struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
}

type orderJSON struct {
	ID     int64      `json:"id"`
	Status Status     `json:"status"`
	Items  []itemJSON `json:"items"`
}

func (h *handler) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Items []itemJSON `json:"items"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed JSON"})
			return
		}
		items := make([]Item, len(req.Items))
		for i, it := range req.Items {
			items[i] = Item(it)
		}
		o, err := h.svc.Create(r.Context(), items)
		h.reply(w, r, http.StatusCreated, o, err)
	})
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		o, err := h.store.Order(r.Context(), id)
		h.reply(w, r, http.StatusOK, o, err)
	})
	mux.HandleFunc("POST /orders/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		o, err := h.store.CancelOrder(r.Context(), id)
		h.reply(w, r, http.StatusOK, o, err)
	})
	return mux
}

func (h *handler) reply(w http.ResponseWriter, r *http.Request, status int, o Order, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	resp := orderJSON{ID: o.ID, Status: o.Status, Items: []itemJSON{}}
	for _, it := range o.Items {
		resp.Items = append(resp.Items, itemJSON(it))
	}
	writeJSON(w, status, resp)
}

// fail — единственное место, где ошибка становится статусом.
// Подробности — в журнал, наружу — только безопасный текст.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve *ValidationError
	status, msg := http.StatusInternalServerError, "internal error"
	switch {
	case errors.As(err, &ve):
		status, msg = http.StatusUnprocessableEntity, ve.Error()
	case errors.Is(err, ErrNotFound):
		status, msg = http.StatusNotFound, "not found"
	case errors.Is(err, ErrOutOfStock):
		status, msg = http.StatusConflict, "out of stock"
	case errors.Is(err, ErrConflict):
		status, msg = http.StatusConflict, "order state does not allow this"
	default:
		h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

const schema = `
CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL,
	available INTEGER NOT NULL CHECK (available >= 0));
CREATE TABLE orders (id INTEGER PRIMARY KEY, status TEXT NOT NULL);
CREATE TABLE order_items (order_id INTEGER NOT NULL REFERENCES orders(id),
	product_id INTEGER NOT NULL REFERENCES products(id), quantity INTEGER NOT NULL,
	PRIMARY KEY (order_id, product_id));
CREATE TABLE outbox (id INTEGER PRIMARY KEY, type TEXT NOT NULL, payload TEXT NOT NULL);
INSERT INTO products (id, name, available) VALUES (1, 'кружка', 5), (2, 'чайник', 1);`

// Сборка — вручную, в `run`: база, хранилище, сервис,
// обработчик, сервер с таймаутами. Никакого DI-контейнера.
func run(ctx context.Context, stdout io.Writer) error {
	dir, err := os.MkdirTemp("", "orders")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	// `_txlock=immediate`: транзакция сразу берёт блокировку
	// записи, писатели SQLite встают в очередь, а не падают.
	db, err := sql.Open("sqlite", filepath.Join(dir, "orders.db")+
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	store := &SQLiteStore{db: db}
	h := &handler{svc: &Service{store: store}, store: store, log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	srv := &http.Server{Handler: h.routes(), ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + ln.Addr().String()
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/orders", `{"items":[{"product_id":2,"quantity":1},{"product_id":1,"quantity":2}]}`},
		{"POST", "/orders", `{"items":[{"product_id":1,"quantity":1},{"product_id":2,"quantity":1}]}`},
		{"POST", "/orders", `{"items":[{"product_id":7,"quantity":1}]}`},
		{"POST", "/orders", `{"items":[{"product_id":1,"quantity":1}],"coupon":"FREE"}`},
		{"GET", "/orders/1", ""},
		{"POST", "/orders/1/cancel", ""},
		{"POST", "/orders/1/cancel", ""},
		{"GET", "/orders/42", ""},
	} {
		req, _ := http.NewRequestWithContext(ctx, c.method, base+c.path, bytes.NewBufferString(c.body))
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Fprintf(stdout, "%-4s %-17s → %d %s", c.method, c.path, resp.StatusCode, body)
	}

	// Второй заказ не прошёл целиком — его кружка вернулась.
	// В outbox — события в порядке изменений; их отправит relay.
	if err := dump(ctx, db, stdout, "склад", `SELECT name || ': ' || available FROM products ORDER BY id`); err != nil {
		return err
	}
	return dump(ctx, db, stdout, "outbox", `SELECT type || ' ' || payload FROM outbox ORDER BY id`)
}

func dump(ctx context.Context, db *sql.DB, w io.Writer, label, query string) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s: %s\n", label, line)
	}
	return rows.Err()
}

func main() {
	if err := run(context.Background(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

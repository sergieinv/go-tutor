package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

// Item — позиция заказа.
type Item struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
}

// Order — заказ с позициями.
type Order struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Items  []Item `json:"items"`
}

var (
	errNotFound   = errors.New("not found")
	errOutOfStock = errors.New("out of stock")
	errUnknown    = errors.New("unknown product")
	errConflict   = errors.New("conflict")
	errInvalid    = errors.New("invalid request")
)

const schema = `
CREATE TABLE IF NOT EXISTS products (id INTEGER PRIMARY KEY, name TEXT NOT NULL, available INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS orders (id INTEGER PRIMARY KEY, status TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS order_items (order_id INTEGER NOT NULL, product_id INTEGER NOT NULL, quantity INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS outbox (id INTEGER PRIMARY KEY, type TEXT NOT NULL, payload TEXT NOT NULL);`

// Service — сервис заказов поверх SQL-базы.
type Service struct {
	db  *sql.DB
	log *slog.Logger
}

// New создаёт таблицы и возвращает сервис.
func New(db *sql.DB) (*Service, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Service{db: db, log: slog.Default()}, nil
}

// Handler возвращает маршруты сервиса.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", s.createOrder)
	mux.HandleFunc("GET /orders", s.listOrders)
	mux.HandleFunc("GET /orders/{id}", s.getOrder)
	mux.HandleFunc("POST /orders/{id}/cancel", s.cancelOrder)
	return mux
}

func (s *Service) createOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []Item `json:"items"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request too large"})
			return
		}
		s.fail(w, r, errInvalid)
		return
	}
	if len(req.Items) == 0 || len(req.Items) > 50 {
		s.fail(w, r, errInvalid)
		return
	}
	for _, it := range req.Items {
		if it.Quantity < 1 || it.Quantity > 1000 {
			s.fail(w, r, errInvalid)
			return
		}
	}
	// Контекст запроса — до самого SQL: клиент ушёл, работа прекращается.
	o, err := s.create(r.Context(), req.Items)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

// withTx: откат при любой ошибке и панике, фиксация при успехе.
func (s *Service) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) create(ctx context.Context, items []Item) (Order, error) {
	o := Order{Status: "reserved", Items: items}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO orders (status) VALUES (?)`, o.Status)
		if err != nil {
			return fmt.Errorf("insert order: %w", err)
		}
		if o.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		for _, it := range items {
			// Атомарный резерв: проверка и списание — один запрос.
			res, err := tx.ExecContext(ctx, `UPDATE products SET available = available - ? WHERE id = ? AND available >= ?`,
				it.Quantity, it.ProductID, it.Quantity)
			if err != nil {
				return fmt.Errorf("reserve %d: %w", it.ProductID, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				var id int64
				err := tx.QueryRowContext(ctx, `SELECT id FROM products WHERE id = ?`, it.ProductID).Scan(&id)
				switch {
				case errors.Is(err, sql.ErrNoRows):
					return errUnknown
				case err != nil:
					return err
				}
				return errOutOfStock
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO order_items (order_id, product_id, quantity) VALUES (?, ?, ?)`,
				o.ID, it.ProductID, it.Quantity); err != nil {
				return fmt.Errorf("insert item: %w", err)
			}
		}
		return addEvent(ctx, tx, "order.created", o.ID)
	})
	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}
	return o, nil
}

func addEvent(ctx context.Context, tx *sql.Tx, typ string, orderID int64) error {
	payload := fmt.Sprintf(`{"order_id":%d}`, orderID)
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox (type, payload) VALUES (?, ?)`, typ, payload); err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (s *Service) getOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.fail(w, r, errNotFound)
		return
	}
	o := Order{ID: id}
	err = s.db.QueryRowContext(r.Context(), `SELECT status FROM orders WHERE id = ?`, id).Scan(&o.Status)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	}
	if err == nil {
		o.Items, err = s.loadItems(r.Context(), id)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Service) loadItems(ctx context.Context, orderID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT product_id, quantity FROM order_items WHERE order_id = ? ORDER BY rowid`, orderID)
	if err != nil {
		return nil, fmt.Errorf("select items: %w", err)
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ProductID, &it.Quantity); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// listOrders — один запрос с JOIN вместо запроса позиций на каждый заказ.
func (s *Service) listOrders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT o.id, o.status, i.product_id, i.quantity
		FROM orders o LEFT JOIN order_items i ON i.order_id = o.id ORDER BY o.id, i.rowid`)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	list := []Order{}
	for rows.Next() {
		var o Order
		var pid, qty sql.NullInt64
		if err := rows.Scan(&o.ID, &o.Status, &pid, &qty); err != nil {
			s.fail(w, r, err)
			return
		}
		if n := len(list); n == 0 || list[n-1].ID != o.ID {
			o.Items = []Item{}
			list = append(list, o)
		}
		if pid.Valid {
			last := &list[len(list)-1]
			last.Items = append(last.Items, Item{ProductID: pid.Int64, Quantity: int(qty.Int64)})
		}
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Service) cancelOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.fail(w, r, errNotFound)
		return
	}
	ctx := r.Context()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'cancelled' WHERE id = ? AND status = 'reserved'`, id)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			var status string
			err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, id).Scan(&status)
			if errors.Is(err, sql.ErrNoRows) {
				return errNotFound
			}
			if err != nil {
				return err
			}
			return errConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE products SET available = available +
			(SELECT SUM(quantity) FROM order_items WHERE order_id = ? AND product_id = products.id)
			WHERE id IN (SELECT product_id FROM order_items WHERE order_id = ?)`, id, id)
		if err != nil {
			return fmt.Errorf("release stock: %w", err)
		}
		return addEvent(ctx, tx, "order.cancelled", id)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items, err := s.loadItems(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, Order{ID: id, Status: "cancelled", Items: items})
}

// fail переводит ошибку в статус. Подробности — в журнал, наружу — общий текст.
func (s *Service) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	msg := "internal error"
	switch {
	case errors.Is(err, errInvalid):
		status, msg = http.StatusBadRequest, errInvalid.Error()
	case errors.Is(err, errNotFound):
		status, msg = http.StatusNotFound, errNotFound.Error()
	case errors.Is(err, errOutOfStock):
		status, msg = http.StatusConflict, errOutOfStock.Error()
	case errors.Is(err, errConflict):
		status, msg = http.StatusConflict, errConflict.Error()
	case errors.Is(err, errUnknown):
		status, msg = http.StatusUnprocessableEntity, errUnknown.Error()
	default:
		s.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

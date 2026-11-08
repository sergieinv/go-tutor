package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	db *sql.DB
}

// New создаёт таблицы и возвращает сервис.
func New(db *sql.DB) (*Service, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Service{db: db}, nil
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
		s.fail(w, errInvalid)
		return
	}
	if len(req.Items) == 0 || len(req.Items) > 50 {
		s.fail(w, errInvalid)
		return
	}
	for _, it := range req.Items {
		if it.Quantity < 1 || it.Quantity > 1000 {
			s.fail(w, errInvalid)
			return
		}
	}
	// Не обрываем запись, если клиент отвалился посреди запроса.
	o, err := s.create(context.Background(), req.Items)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Service) create(ctx context.Context, items []Item) (Order, error) {
	// Сначала проверяем остатки, чтобы не открывать транзакцию зря.
	for _, it := range items {
		var available int
		err := s.db.QueryRowContext(ctx, `SELECT available FROM products WHERE id = ?`, it.ProductID).Scan(&available)
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, errUnknown
		}
		if err != nil {
			return Order{}, err
		}
		if available < it.Quantity {
			return Order{}, errOutOfStock
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO orders (status) VALUES ('reserved')`)
	if err != nil {
		return Order{}, err
	}
	id, _ := res.LastInsertId()
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE products SET available = available - ? WHERE id = ?`, it.Quantity, it.ProductID); err != nil {
			return Order{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_items (order_id, product_id, quantity) VALUES (?, ?, ?)`,
			id, it.ProductID, it.Quantity); err != nil {
			return Order{}, err
		}
	}
	tx.ExecContext(ctx, `INSERT INTO outbox (type, payload) VALUES ('order.created', ?)`, fmt.Sprintf(`{"order_id":%d}`, id))
	if err := tx.Commit(); err != nil {
		return Order{}, err
	}
	return Order{ID: id, Status: "reserved", Items: items}, nil
}

func (s *Service) getOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.fail(w, errNotFound)
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
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Service) loadItems(ctx context.Context, orderID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT product_id, quantity FROM order_items WHERE order_id = ? ORDER BY rowid`, orderID)
	if err != nil {
		return nil, err
	}
	items := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ProductID, &it.Quantity); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Service) listOrders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, status FROM orders ORDER BY id`)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	list := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.Status); err != nil {
			s.fail(w, err)
			return
		}
		list = append(list, o)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	for i := range list {
		if list[i].Items, err = s.loadItems(r.Context(), list[i].ID); err != nil {
			s.fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Service) cancelOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.fail(w, errNotFound)
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		err = errNotFound
	} else if err == nil && status != "reserved" {
		err = errConflict
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE orders SET status = 'cancelled' WHERE id = ?`, id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE products SET available = available +
			(SELECT SUM(quantity) FROM order_items WHERE order_id = ? AND product_id = products.id)
			WHERE id IN (SELECT product_id FROM order_items WHERE order_id = ?)`, id, id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO outbox (type, payload) VALUES ('order.cancelled', ?)`, fmt.Sprintf(`{"order_id":%d}`, id))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	items, err := s.loadItems(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Order{ID: id, Status: "cancelled", Items: items})
}

// fail переводит ошибку в статус ответа.
func (s *Service) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, errNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errOutOfStock), errors.Is(err, errConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, errUnknown):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

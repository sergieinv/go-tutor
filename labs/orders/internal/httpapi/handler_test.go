package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gotutor/labs/orders/internal/httpapi"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/sqlite"
)

// syncBuffer — журнал теста: обработчики пишут в него из горутин сервера.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newSQLiteServer поднимает API поверх настоящего хранилища SQLite.
func newSQLiteServer(t *testing.T) (*httptest.Server, orders.Store) {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "orders.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	svc := orders.NewService(store, time.Now)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := httptest.NewServer(httpapi.New(svc, store.Ping, log))
	t.Cleanup(srv.Close)
	return srv, store
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func do(t *testing.T, method, url, contentType, body string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: b}
}

func decode[T any](t *testing.T, r response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
	return v
}

type orderJSON struct {
	ID         int64  `json:"id"`
	CustomerID int64  `json:"customer_id"`
	Status     string `json:"status"`
	TotalCents int64  `json:"total_cents"`
	Items      []struct {
		ProductID  int64 `json:"product_id"`
		Quantity   int   `json:"quantity"`
		PriceCents int64 `json:"price_cents"`
	} `json:"items"`
}

type productJSON struct {
	ID        int64 `json:"id"`
	Available int   `json:"available"`
}

type errorJSON struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Field     string `json:"field"`
	RequestID string `json:"request_id"`
}

func TestOrderLifecycle(t *testing.T) {
	srv, store := newSQLiteServer(t)
	p, err := store.CreateProduct(t.Context(), orders.NewProduct{Name: "Клавиатура", PriceCents: 499000, Available: 3})
	if err != nil {
		t.Fatal(err)
	}
	productURL := srv.URL + "/products/" + strconv.FormatInt(p.ID, 10)
	orderBody := `{"customer_id": 42, "items": [{"product_id": ` + strconv.FormatInt(p.ID, 10) + `, "quantity": 2}]}`

	// Создание.
	r := do(t, "POST", srv.URL+"/orders", "application/json", orderBody)
	if r.status != http.StatusCreated {
		t.Fatalf("POST /orders: status %d, body %s", r.status, r.body)
	}
	created := decode[orderJSON](t, r)
	if created.Status != "reserved" || created.TotalCents != 2*499000 || created.CustomerID != 42 || len(created.Items) != 1 {
		t.Errorf("created order = %+v", created)
	}
	if loc, want := r.header.Get("Location"), "/orders/"+strconv.FormatInt(created.ID, 10); loc != want {
		t.Errorf("Location = %q, want %q", loc, want)
	}
	orderURL := srv.URL + "/orders/" + strconv.FormatInt(created.ID, 10)

	// Чтение.
	r = do(t, "GET", orderURL, "", "")
	if r.status != http.StatusOK {
		t.Fatalf("GET order: status %d, body %s", r.status, r.body)
	}
	if got := decode[orderJSON](t, r); got.ID != created.ID || got.Status != "reserved" {
		t.Errorf("GET order = %+v", got)
	}
	if got := decode[productJSON](t, do(t, "GET", productURL, "", "")); got.Available != 1 {
		t.Errorf("available after order = %d, want 1", got.Available)
	}

	// Сверх остатка — 409.
	r = do(t, "POST", srv.URL+"/orders", "application/json", orderBody)
	if r.status != http.StatusConflict || decode[errorJSON](t, r).Error != "out_of_stock" {
		t.Fatalf("order over stock: status %d, body %s; want 409 out_of_stock", r.status, r.body)
	}

	// Отмена возвращает остаток; повторная отмена — тот же ответ.
	for range 2 {
		r = do(t, "POST", orderURL+"/cancel", "", "")
		if r.status != http.StatusOK || decode[orderJSON](t, r).Status != "cancelled" {
			t.Fatalf("cancel: status %d, body %s", r.status, r.body)
		}
	}
	if got := decode[productJSON](t, do(t, "GET", productURL, "", "")); got.Available != 3 {
		t.Errorf("available after cancel = %d, want 3", got.Available)
	}
}

func TestRequestErrors(t *testing.T) {
	srv, _ := newSQLiteServer(t)
	valid := `{"customer_id": 1, "items": [{"product_id": 1, "quantity": 1}]}`

	tests := []struct {
		name, method, path, contentType, body string
		wantStatus                            int
		wantCode                              string
		wantField                             string
	}{
		{"wrong content type", "POST", "/orders", "text/plain", valid, 415, "unsupported_media_type", ""},
		{"no content type", "POST", "/orders", "", valid, 415, "unsupported_media_type", ""},
		{"broken json", "POST", "/orders", "application/json", `{"customer_id": 1,`, 400, "invalid_json", ""},
		{"unknown field", "POST", "/orders", "application/json", `{"customer_id": 1, "items": [{"product_id": 1, "quantiy": 1}]}`, 400, "invalid_json", ""},
		{"two objects", "POST", "/orders", "application/json", valid + valid, 400, "invalid_json", ""},
		{"wrong type", "POST", "/orders", "application/json", `{"customer_id": "1", "items": []}`, 400, "invalid_json", ""},
		{"too large", "POST", "/orders", "application/json", `{"customer_id": 1, "items": [` + strings.Repeat(`{"product_id": 1, "quantity": 1},`, 5000) + `]}`, 413, "body_too_large", ""},
		{"validation", "POST", "/orders", "application/json; charset=utf-8", `{"customer_id": 0, "items": []}`, 422, "validation_failed", "customer_id"},
		{"unknown product", "POST", "/orders", "application/json", `{"customer_id": 1, "items": [{"product_id": 999999, "quantity": 1}]}`, 422, "validation_failed", "items[0].product_id"},
		{"bad id", "GET", "/orders/abc", "", "", 400, "invalid_id", ""},
		{"zero id", "GET", "/orders/0", "", "", 400, "invalid_id", ""},
		{"missing order", "GET", "/orders/999999", "", "", 404, "not_found", ""},
		{"cancel missing order", "POST", "/orders/999999/cancel", "", "", 404, "not_found", ""},
		{"missing product", "GET", "/products/999999", "", "", 404, "not_found", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := do(t, tt.method, srv.URL+tt.path, tt.contentType, tt.body)
			if r.status != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", r.status, tt.wantStatus, r.body)
			}
			if ct := r.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			e := decode[errorJSON](t, r)
			if e.Error != tt.wantCode || e.Field != tt.wantField {
				t.Errorf("error = %+v, want code %q field %q", e, tt.wantCode, tt.wantField)
			}
			if e.RequestID == "" || e.RequestID != r.header.Get("X-Request-ID") {
				t.Errorf("request_id = %q, X-Request-ID = %q; want equal and non-empty", e.RequestID, r.header.Get("X-Request-ID"))
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv, _ := newSQLiteServer(t)
	if r := do(t, "DELETE", srv.URL+"/orders/1", "", ""); r.status != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /orders/1: status %d, want 405", r.status)
	}
}

// fakeOrders — подмена домена для сценариев, которых не добиться от
// настоящей базы: внутренняя ошибка и паника.
type fakeOrders struct {
	order func() (orders.Order, error)
}

func (f fakeOrders) CreateOrder(context.Context, orders.CreateOrderRequest) (orders.Order, error) {
	return f.order()
}
func (f fakeOrders) Order(context.Context, int64) (orders.Order, error)       { return f.order() }
func (f fakeOrders) CancelOrder(context.Context, int64) (orders.Order, error) { return f.order() }
func (f fakeOrders) Product(context.Context, int64) (orders.Product, error) {
	return orders.Product{}, nil
}

func TestInternalErrorsDoNotLeak(t *testing.T) {
	const secret = `relation "orders_secret" does not exist (host=db.internal)`
	tests := []struct {
		name    string
		order   func() (orders.Order, error)
		wantLog string
	}{
		{"error", func() (orders.Order, error) { return orders.Order{}, errors.New(secret) }, "orders_secret"},
		{"panic", func() (orders.Order, error) { panic(secret) }, "goroutine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &syncBuffer{}
			log := slog.New(slog.NewJSONHandler(logs, nil))
			ready := func(context.Context) error { return nil }
			srv := httptest.NewServer(httpapi.New(fakeOrders{order: tt.order}, ready, log))
			defer srv.Close()

			r := do(t, "GET", srv.URL+"/orders/1", "", "")
			if r.status != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", r.status)
			}
			if e := decode[errorJSON](t, r); e.Error != "internal" {
				t.Errorf("error code = %q, want internal", e.Error)
			}
			for _, leak := range []string{"orders_secret", "db.internal", "goroutine", ".go:"} {
				if strings.Contains(string(r.body), leak) {
					t.Errorf("response leaks %q: %s", leak, r.body)
				}
			}
			// Подробности — в журнале, вместе с id запроса.
			if !strings.Contains(logs.String(), tt.wantLog) || !strings.Contains(logs.String(), r.header.Get("X-Request-ID")) {
				t.Errorf("log lacks details or request id:\n%s", logs.String())
			}
		})
	}
}

func TestHealthAndReadiness(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	var dbErr error
	var mu sync.Mutex
	ready := func(context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		return dbErr
	}
	srv := httptest.NewServer(httpapi.New(fakeOrders{}, ready, log))
	defer srv.Close()

	if r := do(t, "GET", srv.URL+"/healthz", "", ""); r.status != http.StatusOK {
		t.Errorf("/healthz: status %d", r.status)
	}
	if r := do(t, "GET", srv.URL+"/readyz", "", ""); r.status != http.StatusOK {
		t.Errorf("/readyz with db up: status %d", r.status)
	}

	mu.Lock()
	dbErr = errors.New("connection refused")
	mu.Unlock()
	if r := do(t, "GET", srv.URL+"/readyz", "", ""); r.status != http.StatusServiceUnavailable {
		t.Errorf("/readyz with db down: status %d, want 503", r.status)
	}
	// Liveness от базы не зависит.
	if r := do(t, "GET", srv.URL+"/healthz", "", ""); r.status != http.StatusOK {
		t.Errorf("/healthz with db down: status %d", r.status)
	}
}

package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeStore — товары в памяти. Если block = true, методы ждут
// отмены контекста — как запрос к базе, который не успевает.
type fakeStore struct {
	mu      sync.Mutex
	items   map[int64]Item
	fail    error // если не nil — любой вызов возвращает её
	block   bool
	calls   int
	lastCtx context.Context
}

func newStore() *fakeStore {
	return &fakeStore{items: map[int64]Item{1: {ID: 1, Name: "лампа", Stock: 3}}}
}

func (s *fakeStore) enter(ctx context.Context) error {
	s.mu.Lock()
	s.calls++
	s.lastCtx = ctx
	block, fail := s.block, s.fail
	s.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return fail
}

func (s *fakeStore) Item(ctx context.Context, id int64) (Item, error) {
	if err := s.enter(ctx); err != nil {
		return Item{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return it, nil
}

func (s *fakeStore) Reserve(ctx context.Context, id int64, qty int) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	switch {
	case !ok:
		return ErrNotFound
	case it.Stock < qty:
		return ErrConflict
	}
	it.Stock -= qty
	s.items[id] = it
	return nil
}

func (s *fakeStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type env struct {
	store *fakeStore
	h     http.Handler
	logs  *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := newStore()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	return &env{store: st, h: NewHandler(NewService(st), log), logs: &buf}
}

func (e *env) do(ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, r)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("тело %q — не JSON {\"error\": ...}: %v", rec.Body.String(), err)
	}
	return b.Error
}

func TestGetItem(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t.Context(), "GET", "/items/1", "")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET /items/1: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var it Item
	if err := json.Unmarshal(rec.Body.Bytes(), &it); err != nil || it != (Item{ID: 1, Name: "лампа", Stock: 3}) {
		t.Errorf("GET /items/1: %s (%v)", rec.Body.String(), err)
	}
}

func TestReserve(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(t.Context(), "POST", "/items/1/reserve", `{"qty": 2}`); rec.Code != 204 {
		t.Fatalf("POST reserve qty=2: %d %s, want 204", rec.Code, rec.Body.String())
	}
	if rec := e.do(t.Context(), "POST", "/items/1/reserve", `{"qty": 2}`); rec.Code != 409 || errorBody(t, rec) != "conflict" {
		t.Errorf("POST reserve сверх остатка: %d %s, want 409 conflict", rec.Code, rec.Body.String())
	}
}

func TestStatuses(t *testing.T) {
	tests := []struct {
		method, path, body string
		code               int
		msg                string
	}{
		{"GET", "/items/7", "", 404, "not found"},
		{"GET", "/items/abc", "", 400, "id: must be integer"},
		{"GET", "/items/0", "", 400, "id: must be positive"},
		{"POST", "/items/1/reserve", `{"qty": 0}`, 400, "qty: must be positive"},
		{"POST", "/items/7/reserve", `{"qty": 1}`, 404, "not found"},
		{"POST", "/items/1/reserve", `{"qty":`, 400, "invalid json"},
	}
	for _, tt := range tests {
		e := newEnv(t)
		rec := e.do(t.Context(), tt.method, tt.path, tt.body)
		if rec.Code != tt.code {
			t.Errorf("%s %s %s: %d %s, want %d", tt.method, tt.path, tt.body, rec.Code, rec.Body.String(), tt.code)
			continue
		}
		if got := errorBody(t, rec); got != tt.msg {
			t.Errorf("%s %s %s: error %q, want %q", tt.method, tt.path, tt.body, got, tt.msg)
		}
	}
}

func TestServiceWraps(t *testing.T) {
	svc := NewService(newStore())
	_, err := svc.Item(t.Context(), 42)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "42") {
		t.Errorf("Item(42) error = %v, want обёрнутую ErrNotFound с id в тексте", err)
	}
	err = svc.Reserve(t.Context(), 1, 100)
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "1") {
		t.Errorf("Reserve(1, 100) error = %v, want обёрнутую ErrConflict с id в тексте", err)
	}
}

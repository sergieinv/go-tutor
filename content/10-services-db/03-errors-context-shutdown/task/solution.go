package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// ValidationError — неверный ввод в поле Field.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

type Item struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Stock int    `json:"stock"`
}

// Store — то, что сервису нужно от хранилища.
type Store interface {
	Item(ctx context.Context, id int64) (Item, error)
	Reserve(ctx context.Context, id int64, qty int) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Item(ctx context.Context, id int64) (Item, error) {
	if id <= 0 {
		return Item{}, &ValidationError{Field: "id", Msg: "must be positive"}
	}
	it, err := s.store.Item(ctx, id)
	if err != nil {
		return Item{}, fmt.Errorf("item %d: %w", id, err)
	}
	return it, nil
}

func (s *Service) Reserve(ctx context.Context, id int64, qty int) error {
	if id <= 0 {
		return &ValidationError{Field: "id", Msg: "must be positive"}
	}
	if qty <= 0 {
		return &ValidationError{Field: "qty", Msg: "must be positive"}
	}
	if err := s.store.Reserve(ctx, id, qty); err != nil {
		return fmt.Errorf("reserve item %d: %w", id, err)
	}
	return nil
}

type handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) http.Handler {
	h := &handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", h.item)
	mux.HandleFunc("POST /items/{id}/reserve", h.reserve)
	return mux
}

func (h *handler) item(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.fail(w, r, &ValidationError{Field: "id", Msg: "must be integer"})
		return
	}
	it, err := h.svc.Item(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (h *handler) reserve(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.fail(w, r, &ValidationError{Field: "id", Msg: "must be integer"})
		return
	}
	var body struct {
		Qty int `json:"qty"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := h.svc.Reserve(r.Context(), id, body.Qty); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fail — единственное место, где ошибка превращается в статус.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "conflict"})
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "timeout"})
	default:
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

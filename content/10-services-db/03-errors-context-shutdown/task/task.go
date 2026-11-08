package task

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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

// Item возвращает товар по id.
func (s *Service) Item(ctx context.Context, id int64) (Item, error) {
	// TODO: проверьте id, вызовите хранилище, оберните ошибку
	return Item{}, nil
}

// Reserve резервирует qty единиц товара.
func (s *Service) Reserve(ctx context.Context, id int64, qty int) error {
	// TODO: проверьте id и qty, вызовите хранилище, оберните ошибку
	return nil
}

// NewHandler возвращает HTTP-обработчик поверх сервиса.
func NewHandler(svc *Service, log *slog.Logger) http.Handler {
	// TODO: маршруты GET /items/{id} и POST /items/{id}/reserve
	return http.NewServeMux()
}

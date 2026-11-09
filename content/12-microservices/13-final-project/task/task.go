package task

import (
	"context"
	"errors"
	"time"
	// Пригодятся: "cmp", "fmt", "slices",
	// "google.golang.org/genproto/googleapis/rpc/errdetails",
	// "google.golang.org/grpc/codes", "google.golang.org/grpc/status".
)

// Доменные ошибки «Заказов». ErrCommitUnknown возвращает Store.Save,
// когда упал сам COMMIT и неизвестно, записан ли заказ.
var (
	ErrInvalid       = errors.New("orders: invalid request")
	ErrOutOfStock    = errors.New("orders: out of stock")
	ErrUnavailable   = errors.New("orders: warehouse unavailable")
	ErrCommitUnknown = errors.New("orders: commit outcome unknown")
)

const (
	MaxItems              = 100
	MaxQuantity           = 1000
	DefaultReleaseTimeout = 5 * time.Second
)

// Item — позиция заказа.
type Item struct {
	ProductID int64
	Quantity  int
}

// Order — созданный заказ.
type Order struct {
	ID         int64
	CustomerID int64
	Items      []Item
}

// Warehouse — клиент «Склада» (gRPC). Reserve и Release идемпотентны
// по номеру заказа; ошибки — статусы gRPC, возможно обёрнутые, или любые
// другие (обрыв соединения, отмена).
type Warehouse interface {
	Reserve(ctx context.Context, orderID int64, items []Item) error
	Release(ctx context.Context, orderID int64) error
}

// Store — база «Заказов».
type Store interface {
	// NextID выдаёт номер следующего заказа (последовательность).
	NextID(ctx context.Context) (int64, error)
	// Save записывает заказ и событие OrderCreated в outbox одной транзакцией.
	Save(ctx context.Context, o Order) error
	// Exists сообщает, записан ли заказ.
	Exists(ctx context.Context, id int64) (bool, error)
	// Очередь компенсаций, которые не удались: добавить, прочитать
	// по порядку добавления, убрать выполненную.
	AddPendingRelease(ctx context.Context, orderID int64) error
	PendingReleases(ctx context.Context) ([]int64, error)
	RemovePendingRelease(ctx context.Context, orderID int64) error
}

// Saga — оркестратор создания заказа. ReleaseTimeout <= 0 — DefaultReleaseTimeout.
type Saga struct {
	Store          Store
	Warehouse      Warehouse
	ReleaseTimeout time.Duration
}

// CreateOrder — сага создания заказа: резерв на складе, запись заказа,
// компенсация при сбое. Поведение — в условии задачи.
func (s *Saga) CreateOrder(ctx context.Context, customerID int64, items []Item) (Order, error) {
	return Order{}, errors.New("not implemented")
}

// RetryReleases повторяет компенсации из очереди и возвращает,
// сколько из них выполнено.
func (s *Saga) RetryReleases(ctx context.Context) (int, error) {
	return 0, errors.New("not implemented")
}

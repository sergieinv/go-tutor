package task

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func (s *Saga) timeout() time.Duration {
	if s.ReleaseTimeout > 0 {
		return s.ReleaseTimeout
	}
	return DefaultReleaseTimeout
}

// detached — контекст для работы, которая должна пережить запрос:
// значения ctx сохраняются, отмена — нет, свой таймаут.
func (s *Saga) detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.timeout())
}

func (s *Saga) CreateOrder(ctx context.Context, customerID int64, items []Item) (Order, error) {
	if err := validate(customerID, items); err != nil {
		return Order{}, err
	}
	sorted := slices.Clone(items)
	slices.SortFunc(sorted, func(a, b Item) int { return cmp.Compare(a.ProductID, b.ProductID) })

	id, err := s.Store.NextID(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("create order: next id: %w", err)
	}

	// Шаг 1: резерв.
	if err := s.Warehouse.Reserve(ctx, id, sorted); err != nil {
		switch code := status.Code(err); {
		case code == codes.FailedPrecondition && hasReason(err, "OUT_OF_STOCK"):
			return Order{}, fmt.Errorf("create order %d: %w: %w", id, ErrOutOfStock, err)
		case code == codes.FailedPrecondition || code == codes.InvalidArgument:
			// Склад ответил отказом — резерва точно нет.
			return Order{}, fmt.Errorf("create order %d: reserve: %w", id, err)
		case code == codes.Unavailable || code == codes.DeadlineExceeded:
			err = fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		// Исход неизвестен — компенсируем.
		return Order{}, errors.Join(fmt.Errorf("create order %d: reserve: %w", id, err), s.compensate(ctx, id))
	}

	// Шаг 2: заказ и событие в своей базе.
	order := Order{ID: id, CustomerID: customerID, Items: sorted}
	err = s.Store.Save(ctx, order)
	if err == nil {
		return order, nil
	}
	err = fmt.Errorf("create order %d: save: %w", id, err)
	if errors.Is(err, ErrCommitUnknown) {
		cctx, cancel := s.detached(ctx)
		exists, cerr := s.Store.Exists(cctx, id)
		cancel()
		switch {
		case cerr != nil:
			// Не знаем, записан ли заказ: резерв не трогаем.
			return Order{}, errors.Join(err, fmt.Errorf("check order %d: %w", id, cerr))
		case exists:
			return order, nil
		}
	}
	return Order{}, errors.Join(err, s.compensate(ctx, id))
}

// compensate возвращает резерв, а если склад не ответил — ставит
// возврат в очередь для RetryReleases.
func (s *Saga) compensate(ctx context.Context, id int64) error {
	rctx, cancel := s.detached(ctx)
	err := s.Warehouse.Release(rctx, id)
	cancel()
	if err == nil {
		return nil
	}
	err = fmt.Errorf("release %d: %w", id, err)
	qctx, cancel := s.detached(ctx)
	defer cancel()
	if qerr := s.Store.AddPendingRelease(qctx, id); qerr != nil {
		return errors.Join(err, fmt.Errorf("queue release %d: %w", id, qerr))
	}
	return err
}

func (s *Saga) RetryReleases(ctx context.Context) (int, error) {
	ids, err := s.Store.PendingReleases(ctx)
	if err != nil {
		return 0, fmt.Errorf("pending releases: %w", err)
	}
	var errs []error
	n := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		rctx, cancel := context.WithTimeout(ctx, s.timeout())
		err := s.Warehouse.Release(rctx, id)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("release %d: %w", id, err))
			continue
		}
		if err := s.Store.RemovePendingRelease(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("remove pending %d: %w", id, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

func hasReason(err error, reason string) bool {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() == reason {
			return true
		}
	}
	return false
}

func validate(customerID int64, items []Item) error {
	if customerID <= 0 {
		return fmt.Errorf("%w: customer_id must be positive", ErrInvalid)
	}
	if len(items) == 0 || len(items) > MaxItems {
		return fmt.Errorf("%w: need 1..%d items", ErrInvalid, MaxItems)
	}
	seen := make(map[int64]bool, len(items))
	for i, it := range items {
		switch {
		case it.ProductID <= 0:
			return fmt.Errorf("%w: items[%d].product_id must be positive", ErrInvalid, i)
		case it.Quantity < 1 || it.Quantity > MaxQuantity:
			return fmt.Errorf("%w: items[%d].quantity must be 1..%d", ErrInvalid, i, MaxQuantity)
		case seen[it.ProductID]:
			return fmt.Errorf("%w: items[%d].product_id duplicated", ErrInvalid, i)
		}
		seen[it.ProductID] = true
	}
	return nil
}

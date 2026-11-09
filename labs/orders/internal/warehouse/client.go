// Package warehouse — клиент сервиса «Склад» для домена «Заказов»:
// реализует orders.Warehouse поверх gRPC WarehouseService.
//
// Устойчивость вызова собрана здесь явно, слоями (снаружи внутрь):
//
//	дедлайн на весь вызов → повторы (только UNAVAILABLE) → выключатель → gRPC
//
// Повторы безопасны, потому что Reserve и Release идемпотентны по номеру
// заказа. Выключатель стоит внутри повторов: каждая попытка — отдельный
// замер, и когда он размыкается посреди серии, оставшиеся попытки
// отклоняются мгновенно (ErrOpen не повторяется).
package warehouse

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/platform/resilience"
)

// Config — настройки клиента. Нулевые поля заменяются значениями по
// умолчанию; Sleep, Now и Jitter внедряются в тестах.
type Config struct {
	// CallTimeout — дедлайн на вызов со всеми повторами; 0 — 3 с. Если у
	// входящего запроса дедлайн раньше, действует он: context.WithTimeout
	// никогда не продлевает родительский.
	CallTimeout time.Duration
	// Retry — повторы; MaxAttempts 0 — 3 попытки, паузы 50 мс → 1 с.
	Retry resilience.Retry
	// Breaker — выключатель; FailureThreshold 0 — 5 неудач подряд,
	// OpenTimeout 0 — 5 с.
	Breaker resilience.BreakerConfig
	// Registerer — куда зарегистрировать метрики; nil — без метрик.
	Registerer prometheus.Registerer
}

// Client реализует orders.Warehouse.
type Client struct {
	api     warehousev1.WarehouseServiceClient
	timeout time.Duration
	retry   resilience.Retry
	breaker *resilience.Breaker
}

var _ orders.Warehouse = (*Client)(nil)

// New создаёт клиент поверх соединения conn (grpcx.NewClient в main,
// bufconn в тестах).
func New(conn grpc.ClientConnInterface, cfg Config) *Client {
	if cfg.CallTimeout == 0 {
		cfg.CallTimeout = 3 * time.Second
	}
	m := newMetrics(cfg.Registerer)

	retry := cfg.Retry
	if retry.MaxAttempts == 0 {
		retry.MaxAttempts = 3
		retry.BaseDelay = 50 * time.Millisecond
		retry.MaxDelay = time.Second
	}
	retry.Retryable = retryable
	userOnRetry := retry.OnRetry
	retry.OnRetry = func(attempt int, delay time.Duration, err error) {
		m.retries.Inc()
		if userOnRetry != nil {
			userOnRetry(attempt, delay, err)
		}
	}

	bc := cfg.Breaker
	if bc.FailureThreshold == 0 {
		bc.FailureThreshold = 5
	}
	if bc.OpenTimeout == 0 {
		bc.OpenTimeout = 5 * time.Second
	}
	bc.IsFailure = isFailure
	userOnChange := bc.OnStateChange
	bc.OnStateChange = func(from, to resilience.State) {
		m.breakerState.Set(float64(to))
		if userOnChange != nil {
			userOnChange(from, to)
		}
	}

	return &Client{
		api:     warehousev1.NewWarehouseServiceClient(conn),
		timeout: cfg.CallTimeout,
		retry:   retry,
		breaker: resilience.NewBreaker(bc),
	}
}

// Reserve резервирует позиции заказа.
func (c *Client) Reserve(ctx context.Context, orderID int64, items []orders.ItemRequest) error {
	req := &warehousev1.ReserveRequest{OrderId: orderID, Items: make([]*warehousev1.ReserveItem, len(items))}
	for i, it := range items {
		req.Items[i] = &warehousev1.ReserveItem{ProductId: it.ProductID, Quantity: int32(it.Quantity)}
	}
	return c.call(ctx, func(ctx context.Context) error {
		_, err := c.api.Reserve(ctx, req)
		return err
	})
}

// Release возвращает резерв заказа.
func (c *Client) Release(ctx context.Context, orderID int64) error {
	return c.call(ctx, func(ctx context.Context) error {
		_, err := c.api.Release(ctx, &warehousev1.ReleaseRequest{OrderId: orderID})
		return err
	})
}

func (c *Client) call(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	err := c.retry.Do(ctx, func(ctx context.Context) error {
		return c.breaker.Do(ctx, fn)
	})
	return toDomain(err)
}

// retryable — повторяем только UNAVAILABLE: сервер не принял вызов
// (перезапуск, обрыв соединения, перегрузка). DEADLINE_EXCEEDED не
// повторяем — дедлайн общий на все попытки, он уже истёк. Ошибки клиента
// (INVALID_ARGUMENT) и бизнес-отказы (FAILED_PRECONDITION) повтор не
// исправит.
func retryable(err error) bool {
	return status.Code(err) == codes.Unavailable && !errors.Is(err, resilience.ErrOpen)
}

// isFailure — что характеризует здоровье склада. Отмена вызывающим, ошибки
// клиента и бизнес-отказы выключатель не размыкают.
func isFailure(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Internal, codes.Unknown, codes.ResourceExhausted:
		return true
	}
	return false
}

// errorDomain — домен ErrorInfo «Склада»; reason без домена не однозначен.
const errorDomain = "warehouse.gotutor.local"

// toDomain переводит ошибку gRPC в доменную — в одном месте. Решение
// принимается по коду и ErrorInfo.reason, а не по тексту сообщения: текст
// для людей и может меняться.
func toDomain(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, resilience.ErrOpen) {
		return fmt.Errorf("%w: circuit breaker is open", orders.ErrWarehouseUnavailable)
	}
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("warehouse: %w", err)
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded:
		return fmt.Errorf("%w: %s", orders.ErrWarehouseUnavailable, st.Message())
	case codes.FailedPrecondition:
		if info := errorInfo(st); info != nil && info.GetDomain() == errorDomain && info.GetReason() == "OUT_OF_STOCK" {
			return &orders.OutOfStockError{ProductIDs: outOfStockProducts(st)}
		}
	case codes.Canceled:
		return fmt.Errorf("warehouse call: %w", context.Canceled)
	}
	return fmt.Errorf("warehouse: %s: %s", st.Code(), st.Message())
}

func errorInfo(st *status.Status) *errdetails.ErrorInfo {
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

// outOfStockProducts достаёт id товаров из PreconditionFailure:
// нарушения вида {type: "STOCK", subject: "products/3"}.
func outOfStockProducts(st *status.Status) []int64 {
	var ids []int64
	for _, d := range st.Details() {
		pf, ok := d.(*errdetails.PreconditionFailure)
		if !ok {
			continue
		}
		for _, v := range pf.GetViolations() {
			if v.GetType() != "STOCK" {
				continue
			}
			if id, err := strconv.ParseInt(strings.TrimPrefix(v.GetSubject(), "products/"), 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids
}

type metrics struct {
	retries      prometheus.Counter
	breakerState prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		retries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_warehouse_retries_total",
			Help: "Retried warehouse calls (UNAVAILABLE).",
		}),
		breakerState: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "orders_warehouse_breaker_state",
			Help: "Warehouse circuit breaker state: 0 closed, 1 open, 2 half-open.",
		}),
	}
	if reg != nil {
		reg.MustRegister(m.retries, m.breakerState)
	}
	return m
}

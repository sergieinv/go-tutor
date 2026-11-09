package stock

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Service — операции склада: резерв, отмена резерва, остаток.
type Service struct {
	store   Store
	now     func() time.Time
	metrics *Metrics
}

// NewService собирает сервис. now — часы (в тестах фиксированные);
// metrics == nil — без метрик.
func NewService(store Store, now func() time.Time, metrics *Metrics) *Service {
	return &Service{store: store, now: now, metrics: metrics}
}

// Результаты операций — значения метки result доменных метрик.
const (
	resultReserved        = "reserved"
	resultDuplicate       = "duplicate"
	resultOutOfStock      = "out_of_stock"
	resultReleased        = "released"
	resultMismatch        = "mismatch"
	resultAlreadyReleased = "already_released"
	resultTombstone       = "tombstone"
)

// Reserve резервирует все позиции заказа или ни одной.
//
// Повтор с теми же позициями возвращает существующий резерв и остатки не
// трогает. Ошибки: *OutOfStockError, ErrReservationReleased,
// ErrReservationMismatch. Корректность запроса (позиции есть, количества
// положительные, товары не повторяются) проверил protovalidate по
// контракту — домен её не дублирует.
func (s *Service) Reserve(ctx context.Context, orderID int64, items []Item) (Reservation, error) {
	want := sortedItems(items)
	at := s.now().UTC().Truncate(time.Microsecond) // DATETIME(6) хранит микросекунды

	var (
		res    Reservation
		result string
	)
	// fn может выполниться несколько раз (повтор транзакции в MySQL):
	// каждая попытка заново присваивает res и result.
	err := s.store.InTx(ctx, func(ctx context.Context, tx Tx) error {
		res, result = Reservation{}, ""

		// Сначала — строки остатков, по возрастанию product_id. Единый
		// порядок блокировок у всех транзакций исключает взаимную
		// блокировку встречных резервов (1,2) и (2,1), а конкурирующие
		// резервы одного товара выстраиваются в очередь ещё до таблицы
		// резервов.
		var shortages []Shortage
		for _, it := range want {
			l, ok, err := tx.LockLevel(ctx, it.ProductID)
			if err != nil {
				return err
			}
			if !ok || l.Available < it.Quantity {
				shortages = append(shortages, Shortage{
					ProductID: it.ProductID,
					Requested: it.Quantity,
					Available: l.Available, // строки нет — 0
				})
			}
		}

		// Затем — «занять» order_id: вставить резерв или, если он уже
		// есть, заблокировать существующий. Нехватку проверяем после: повтор
		// уже выполненного резерва должен вернуть резерв, а не OUT_OF_STOCK
		// (товар списан первым вызовом).
		res = Reservation{OrderID: orderID, Status: StatusReserved, Items: want}
		existing, created, err := tx.ClaimReservation(ctx, res, at)
		if err != nil {
			return err
		}
		if !created {
			switch {
			case existing.Status == StatusReleased:
				res, result = Reservation{}, resultReleased
				return ErrReservationReleased
			case slices.Equal(existing.Items, want):
				// Идемпотентный повтор: ответ потерялся, клиент спросил
				// ещё раз. Остатки не трогаем.
				res, result = existing, resultDuplicate
				return nil
			default:
				res, result = Reservation{}, resultMismatch
				return ErrReservationMismatch
			}
		}
		if len(shortages) > 0 {
			// Вставленный резерв исчезнет вместе с откатом транзакции.
			res, result = Reservation{}, resultOutOfStock
			return &OutOfStockError{Shortages: shortages}
		}

		for _, it := range want {
			if err := tx.AddLevel(ctx, it.ProductID, -it.Quantity, it.Quantity, at); err != nil {
				return err
			}
		}
		result = resultReserved
		return nil
	})
	// Метрика — по последней попытке и только если исход известен: сбой
	// базы при фиксации не должен посчитаться как «reserved».
	if err == nil || isDomain(err) {
		s.metrics.reservation(result)
	}
	if err != nil {
		return Reservation{}, wrap("reserve", err)
	}
	return res, nil
}

// Release отменяет резерв и возвращает товар в доступный остаток.
//
// Повторный Release и Release без резерва — не ошибка. Release без резерва
// оставляет «надгробие» (резерв в статусе released без позиций): Reserve,
// опоздавший из-за сети или повтора клиента, будет отклонён, а не
// зарезервирует товар для уже отменённого заказа навсегда.
func (s *Service) Release(ctx context.Context, orderID int64) (Reservation, error) {
	at := s.now().UTC().Truncate(time.Microsecond)

	var (
		res    Reservation
		result string
	)
	err := s.store.InTx(ctx, func(ctx context.Context, tx Tx) error {
		res, result = Reservation{}, ""

		// Резерва нет — вставится «надгробие»; есть — заблокируется.
		tombstone := Reservation{OrderID: orderID, Status: StatusReleased}
		existing, created, err := tx.ClaimReservation(ctx, tombstone, at)
		if err != nil {
			return err
		}
		switch {
		case created:
			res, result = tombstone, resultTombstone
			return nil
		case existing.Status == StatusReleased:
			res, result = existing, resultAlreadyReleased
			return nil
		}

		// Позиции уже упорядочены по product_id — тот же порядок
		// блокировок строк остатков, что у Reserve.
		for _, it := range existing.Items {
			if err := tx.AddLevel(ctx, it.ProductID, it.Quantity, -it.Quantity, at); err != nil {
				return err
			}
		}
		if err := tx.SetReservationStatus(ctx, orderID, StatusReleased, at); err != nil {
			return err
		}
		existing.Status = StatusReleased
		res, result = existing, resultReleased
		return nil
	})
	if err != nil {
		return Reservation{}, wrap("release", err)
	}
	s.metrics.release(result)
	return res, nil
}

// Level возвращает остаток товара. Неизвестный товар — нулевой остаток, а
// не ошибка: каталогом склад не владеет.
func (s *Service) Level(ctx context.Context, productID int64) (Level, error) {
	l, err := s.store.Level(ctx, productID)
	if err != nil {
		return Level{}, wrap("get stock", err)
	}
	return l, nil
}

// wrap добавляет контекст к ошибкам хранилища, а доменные возвращает как
// есть: их текст уже понятен, а errors.Is/As работает в обоих случаях.
func wrap(op string, err error) error {
	if isDomain(err) {
		return err
	}
	return fmt.Errorf("%s: %w", op, err)
}

// isDomain — ошибка бизнес-правила, а не сбой инфраструктуры.
func isDomain(err error) bool {
	var oos *OutOfStockError
	return errors.Is(err, ErrReservationReleased) || errors.Is(err, ErrReservationMismatch) || errors.As(err, &oos)
}

// Metrics — доменные метрики склада.
type Metrics struct {
	reservations *prometheus.CounterVec
	releases     *prometheus.CounterVec
}

// NewMetrics регистрирует метрики в reg. Все значения метки result
// создаются сразу: ряд с нулём виден в Prometheus до первого события, и
// rate() по нему считается без «дыр».
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		reservations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "warehouse_reservations_total",
			Help: "Reserve calls by result: reserved, duplicate, out_of_stock, released, mismatch.",
		}, []string{"result"}),
		releases: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "warehouse_releases_total",
			Help: "Release calls by result: released, already_released, tombstone.",
		}, []string{"result"}),
	}
	for _, r := range []string{resultReserved, resultDuplicate, resultOutOfStock, resultReleased, resultMismatch} {
		m.reservations.WithLabelValues(r)
	}
	for _, r := range []string{resultReleased, resultAlreadyReleased, resultTombstone} {
		m.releases.WithLabelValues(r)
	}
	reg.MustRegister(m.reservations, m.releases)
	return m
}

// Методы с nil-получателем — допустимый в Go приём: «метрики выключены»
// без проверок на каждом вызове.
func (m *Metrics) reservation(result string) {
	if m != nil {
		m.reservations.WithLabelValues(result).Inc()
	}
}

func (m *Metrics) release(result string) {
	if m != nil {
		m.releases.WithLabelValues(result).Inc()
	}
}

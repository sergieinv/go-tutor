package orders

import (
	"context"
	"time"
)

// Store — хранилище, каким его видит сервис. Реализации — в
// internal/store/{postgres,sqlite}; общий контрактный набор тестов —
// internal/storetest.
//
// Методы Store работают вне транзакции (каждый запрос — сам по себе).
// Всё, что должно быть атомарным, делается через Begin и методы Tx.
type Store interface {
	// Begin открывает транзакцию. Вызывающий обязан завершить её Commit
	// или Rollback; идиома — defer tx.Rollback(ctx) сразу после Begin.
	Begin(ctx context.Context) (Tx, error)
	// NextOrderID выдаёт номер для нового заказа. Номер нужен раньше, чем
	// заказ записан: это ключ идемпотентности резерва на складе. Номера не
	// повторяются никогда; пропуски (заказ не создан) — норма.
	NextOrderID(ctx context.Context) (int64, error)
	// Order возвращает заказ с позициями или ошибку с ErrNotFound.
	Order(ctx context.Context, id int64) (Order, error)
	// ListOrders возвращает до limit заказов покупателя с id < beforeID
	// (beforeID = 0 — без ограничения), новые первыми, с позициями.
	ListOrders(ctx context.Context, customerID, beforeID int64, limit int) ([]Order, error)
	// ProductsByIDs возвращает найденные товары каталога, упорядоченные по
	// ID. Отсутствующие id просто не попадают в результат.
	ProductsByIDs(ctx context.Context, ids []int64) ([]Product, error)
	// CreateProduct добавляет товар в каталог (тесты и начальная загрузка).
	CreateProduct(ctx context.Context, p NewProduct) (Product, error)
	// Ping проверяет связь с базой (для /readyz).
	Ping(ctx context.Context) error
}

// Tx — операции внутри одной транзакции. Транзакция — явный объект,
// который передаётся из метода в метод, а не «окружающий» контекст,
// как TransactionScope в .NET: по сигнатуре видно, что выполняется
// атомарно.
type Tx interface {
	// Commit фиксирует транзакцию.
	Commit(ctx context.Context) error
	// Rollback откатывает транзакцию. После Commit — безопасный no-op
	// (возвращает ошибку «уже завершена», её можно не проверять).
	Rollback(ctx context.Context) error
	// CreateOrder сохраняет заказ (с уже выданным o.ID) и его позиции.
	CreateOrder(ctx context.Context, o Order) error
	// LockOrder читает заказ с позициями и блокирует его строку до конца
	// транзакции (SELECT … FOR UPDATE там, где он есть). Ошибка с
	// ErrNotFound, если заказа нет.
	LockOrder(ctx context.Context, id int64) (Order, error)
	// UpdateOrderStatus меняет статус, только если текущий равен from;
	// иначе — ошибка с ErrConflict.
	UpdateOrderStatus(ctx context.Context, id int64, from, to Status, at time.Time) error
	// AddEvent пишет событие в outbox.
	AddEvent(ctx context.Context, e Event) error
}

// Warehouse — сервис «Склад», каким его видит домен. Реализация —
// gRPC-клиент internal/warehouse (повторы, дедлайн, выключатель, перевод
// ошибок в доменные).
//
// Оба метода идемпотентны по orderID: повтор не меняет остатки второй раз,
// а Release без резерва оставляет «надгробие», и опоздавший Reserve того же
// заказа будет отклонён.
type Warehouse interface {
	// Reserve резервирует все позиции или ни одной. Не хватает товара —
	// *OutOfStockError; склад не ответил — ошибка с ErrWarehouseUnavailable
	// (тогда неизвестно, сделан ли резерв).
	Reserve(ctx context.Context, orderID int64, items []ItemRequest) error
	// Release возвращает резерв заказа на склад.
	Release(ctx context.Context, orderID int64) error
}

// Observer получает сведения о ходе саги — для метрик. Реализация — в main
// (Prometheus); nil в Options — ничего не сообщать.
type Observer interface {
	// Released сообщает исход возврата резерва: reason — почему
	// (ReleaseReason*), err — ошибка или nil. Неудачная компенсация — резерв,
	// который остался висеть: повод для алерта.
	Released(reason string, err error)
}

// Причины возврата резерва (метка метрики).
const (
	// ReleaseReasonReserveFailed — резерв завершился ошибкой с неизвестным
	// исходом (таймаут, обрыв): склад мог успеть зарезервировать.
	ReleaseReasonReserveFailed = "reserve_failed"
	// ReleaseReasonWriteFailed — резерв сделан, но заказ не записался.
	ReleaseReasonWriteFailed = "write_failed"
	// ReleaseReasonCancel — заказ отменён.
	ReleaseReasonCancel = "cancel"
)

package orders

import (
	"context"
	"time"
)

// Store — хранилище, каким его видит сервис. Реализации — в
// internal/store/{postgres,mysql,sqlite}; общий контрактный набор тестов —
// internal/storetest.
//
// Методы Store работают вне транзакции (каждый запрос — сам по себе).
// Всё, что должно быть атомарным, делается через Begin и методы Tx.
type Store interface {
	// Begin открывает транзакцию. Вызывающий обязан завершить её Commit
	// или Rollback; идиома — defer tx.Rollback(ctx) сразу после Begin.
	Begin(ctx context.Context) (Tx, error)

	// Order возвращает заказ с позициями или ошибку с ErrNotFound.
	Order(ctx context.Context, id int64) (Order, error)
	// Product возвращает товар с остатком или ошибку с ErrNotFound.
	Product(ctx context.Context, id int64) (Product, error)
	// CreateProduct добавляет товар и его остаток (в одной транзакции).
	// Каталог ведёт не этот сервис: метод нужен тестам и начальной загрузке,
	// в модуле 11 он уйдёт в «Склад».
	CreateProduct(ctx context.Context, p NewProduct) (Product, error)
	// PendingEvents возвращает до limit неопубликованных событий outbox
	// по порядку. Сейчас его читают тесты, в модуле 12 — публикатор.
	PendingEvents(ctx context.Context, limit int) ([]Event, error)
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

	// ProductsByIDs возвращает найденные товары, упорядоченные по ID.
	// Отсутствующие id просто не попадают в результат.
	ProductsByIDs(ctx context.Context, ids []int64) ([]Product, error)
	// ReserveStock атомарно уменьшает остаток на qty. Если остатка не
	// хватает (или записи об остатке нет) — ошибка с ErrOutOfStock,
	// остаток не меняется.
	ReserveStock(ctx context.Context, productID int64, qty int) error
	// ReleaseStock возвращает qty на склад.
	ReleaseStock(ctx context.Context, productID int64, qty int) error

	// CreateOrder сохраняет заказ с позициями и возвращает его ID.
	// Поле o.ID игнорируется.
	CreateOrder(ctx context.Context, o Order) (int64, error)
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

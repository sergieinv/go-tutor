package orders

import "context"

// Модуль 14: под нагрузкой одно хранилище перестаёт быть «одной базой».
// Каталог читается через кеш, заказы покупателя живут на своём шарде,
// история читается с реплики. Домен видит это через три интерфейса ниже;
// реализации — internal/catalog, internal/shard, internal/replica, сборка
// — в main. Без них (nil в Options) сервис работает как в модулях 10–12:
// всё в одном Store.

// Catalog — чтение каталога товаров. Реализации: Store (база) и
// internal/catalog (кеш в Valkey поверх базы).
type Catalog interface {
	// ProductsByIDs возвращает найденные товары, упорядоченные по ID.
	// Отсутствующие id просто не попадают в результат.
	ProductsByIDs(ctx context.Context, ids []int64) ([]Product, error)
}

// Router знает, на каком хранилище (шарде) живут заказы. Все заказы одного
// покупателя — на одном шарде: история читается одним запросом к одной
// базе, а транзакция заказа не выходит за пределы шарда.
type Router interface {
	// ForCustomer — шард покупателя: туда пишутся его новые заказы и оттуда
	// читается его история.
	ForCustomer(customerID int64) Store
	// ForOrder — шард заказа по его номеру (номер шарда зашит в номер
	// заказа, опрашивать все шарды не нужно). Неизвестный шард — ошибка с
	// ErrNotFound.
	ForOrder(orderID int64) (Store, error)
}

// Position — токен согласованности (read-your-writes): позиция записи в
// базе, после которой клиент хочет читать. Для домена — непрозрачная
// строка; понимает её реализация History. Пустая — «любая свежесть».
type Position string

// History — чтение истории покупателя, возможно с реплики.
type History interface {
	// ListOrders — как Store.ListOrders, но не старее позиции after: если
	// реплика ещё не применила эту позицию, чтение идёт на основную базу.
	// Некорректный токен — *ValidationError.
	ListOrders(ctx context.Context, customerID, beforeID int64, limit int, after Position) ([]Order, error)
	// WritePosition — позиция основной базы шарда покупателя сразу после
	// его записи. Ошибок нет: если позицию узнать не удалось, реализация
	// возвращает токен «читать с основной».
	WritePosition(ctx context.Context, customerID int64) Position
}

// singleRouter — все заказы в одном хранилище (режим по умолчанию).
type singleRouter struct{ store Store }

func (r singleRouter) ForCustomer(int64) Store       { return r.store }
func (r singleRouter) ForOrder(int64) (Store, error) { return r.store, nil }

// primaryHistory — история с основной базы шарда; позиции не нужны.
type primaryHistory struct{ router Router }

func (h primaryHistory) ListOrders(ctx context.Context, customerID, beforeID int64, limit int, _ Position) ([]Order, error) {
	return h.router.ForCustomer(customerID).ListOrders(ctx, customerID, beforeID, limit)
}

func (primaryHistory) WritePosition(context.Context, int64) Position { return "" }

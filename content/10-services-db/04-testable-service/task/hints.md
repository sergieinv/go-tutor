## Подсказка 1

Фейк должен повторять **поведение** SQL-реализации, а не её устройство: прочитайте `SQLiteStore` и выпишите, что видно снаружи — порядок проверок (контекст → аргументы → существование → остаток), какие ошибки и когда, как выдаются id (с 1, по порядку, неудачный `Create` id не тратит), что `List` отсортирован по id. Схема тоже часть поведения: `UNIQUE` на `name` — это `ErrDuplicate`.

## Подсказка 2

Состояние: `map[int64]Item` по id, `map[string]int64` для проверки уникальности имени, счётчик `lastID` и `sync.Mutex` вокруг всего — тесты зовут методы из многих горутин. `Item` — значение без указателей, так что `Get` и элементы `List` уже копии; но `List` должен собрать новый срез и отсортировать его: порядок обхода map случайный.

## Подсказка 3

```go
func (m *MemStore) Reserve(ctx context.Context, id int64, qty int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if qty <= 0 {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	switch {
	case !ok:
		return ErrNotFound
	case it.Qty < qty:
		return ErrInsufficient
	}
	it.Qty -= qty
	m.items[id] = it
	return nil
}
```

`Create` — так же: контекст, аргументы, под мьютексом проверка `byName`, затем `m.lastID++`. `List` — `make([]Item, 0, len(m.items))`, обход map и `slices.SortFunc` по `ID`.

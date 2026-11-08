## Подсказка 1

Всё, что сейчас лежит в пакетных переменных, переезжает в структуру: `Desk` хранит `now`, `newID`, `map[string]Ticket` и `sync.Mutex`. Map создайте в `NewDesk` (запись в nil-map паникует), там же подставьте `time.Now` и `rand.Text` вместо `nil`. Пакетный `seq` больше не нужен: номер выдаёт генератор `newID`. В `Open` сначала проверьте вход и только потом вызывайте `d.newID()` и `d.now()` — по одному разу.

## Подсказка 2

Порядок обхода map случаен, а `sort.Slice` по одному приоритету оставляет равные элементы в этом случайном порядке. Нужен полный ключ сортировки:

```go
slices.SortFunc(list, func(a, b Ticket) int {
	return cmp.Or(
		cmp.Compare(b.Priority, a.Priority), // по убыванию: b и a поменяны местами
		a.Created.Compare(b.Created),
		cmp.Compare(a.ID, b.ID),
	)
})
```

`cmp.Or` возвращает первый ненулевой результат. Срез собирайте заново под мьютексом — он и будет копией.

## Подсказка 3

`FakeClock` — `time.Time` под `sync.Mutex`: `Now` читает его под блокировкой, `Advance` — `c.now = c.now.Add(d)`. В `Expire` возьмите `now := d.now()` один раз и сравнивайте `now.Sub(t.Created) >= maxAge`; удалять из map во время `range` по ней в Go можно. `SeqIDs` — замыкание со своим счётчиком:

```go
func SeqIDs(prefix string) func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		n++
		id := n
		mu.Unlock()
		return prefix + "-" + strconv.Itoa(id)
	}
}
```

Вместо мьютекса подойдёт `atomic.Int64` и `strconv.FormatInt(c.Add(1), 10)`.

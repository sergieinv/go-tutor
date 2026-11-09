# Конкурентность

Обзор модуля для показа команде: как Go обходится без async/await.

---

## Нет async/await

- Код пишется синхронно, конкурентность — оператор `go f()`.
- Горутина стоит несколько килобайт стека. Тысячи горутин — норма, а не повод для пула.
- Вместо `Task<T>` — канал, вместо `CancellationToken` — `context.Context`.
- `go f()` ничего не возвращает: ни результата, ни способа дождаться или отменить. И `main` не ждёт горутины.

---

## Горутины поверх потоков: планировщик GMP

G — горутина, M — поток ОС, P — право выполнять Go-код (их `GOMAXPROCS`). Шаги: очереди P, work stealing, handoff при системном вызове, вытеснение.

@viz 05-concurrency/01-goroutines

---

## Канал — и данные, и синхронизация

- `make(chan T)` — точка встречи: отправитель ждёт получателя, и наоборот.
- `make(chan T, n)` — буфер на `n` значений; бесконечного буфера нет.
- Закрывает **отправитель**, ровно один раз; `for v := range ch` читает до закрытия.

| C# | Go |
|---|---|
| `Channel.CreateBounded<T>(n)` | `make(chan T, n)`; ёмкость 0 — рандеву |
| `writer.Complete()` | `close(ch)` |
| `await foreach (… ReadAllAsync())` | `for v := range ch` |
| `ChannelReader<T>` | `<-chan T` — направление проверяет компилятор |

---

## Канал изнутри

`runtime.hchan`: кольцевой буфер, очереди ждущих отправителей и получателей, прямая передача в стек и `close`.

@viz 05-concurrency/02-channels

---

## `select` — `Task.WhenAny` для каналов

```go
select {
case msg := <-results:
	handle(msg)
case <-ctx.Done():
	return ctx.Err()
case <-time.After(time.Second):
	return ErrTimeout
}
```

Если готовы несколько веток, выбор случаен. Порядок `case` не задаёт приоритет.

---

## `select` по реальному запуску

Нажмите «Запустить и построить»: программа выполнится в runner с трассировкой, и шаги построятся по тому, что произошло на самом деле.

@viz 05-concurrency/07-select

---

## Таймаут прекращает ожидание, а не работу

```go
ch := make(chan string, 1) // буфер 1: исполнитель не застрянет после таймаута
go func() { ch <- slowCall() }()
select {
case v := <-ch:
	return v, nil
case <-time.After(time.Second):
	return "", ErrTimeout
}
```

Горутина, навсегда заблокированная на канале, — утечка: GC её не собирает. Запуская горутину, сразу решите, как и когда она закончится.

---

## Примитивы `sync`: знакомые задачи

| C# | Go |
|---|---|
| `await Task.WhenAll(tasks)` | `sync.WaitGroup`: `wg.Go(f)` (Go 1.25) и `wg.Wait()` |
| `lock (obj) { … }` | `mu.Lock()` + `defer mu.Unlock()`; **не** реентерабелен |
| `Interlocked.Increment(ref x)` | `x.Add(1)` у `atomic.Int64` |
| `Lazy<T>` | `sync.OnceValue(f)` |

- Типы `sync` — значения: копировать нельзя, методы — на указателе (`go vet` ловит).
- Атомик защищает одно значение; инвариант из нескольких полей — под мьютексом.

---

## `context.Context` — это `CancellationToken`

| C# | Go |
|---|---|
| `CancellationToken` | `ctx.Done()` + `ctx.Err()` |
| `CreateLinkedTokenSource(parent)` | `ctx, cancel := context.WithCancel(parent)` |
| `CancelAfter(t)` | `context.WithTimeout(parent, t)` |
| `ThrowIfCancellationRequested()` | `return ctx.Err()` — обычная ошибка |

- `ctx` — первый параметр функции, а не поле структуры.
- `defer cancel()` сразу после создания; отмена родителя отменяет всех потомков.

---

## `errgroup` — `WhenAll` с ошибками и отменой

```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(3) // как MaxDegreeOfParallelism
for _, u := range urls {
	g.Go(func() error {
		return fetch(ctx, u) // контекст группы: отменится при первой ошибке
	})
}
err := g.Wait() // первая ненулевая ошибка
```

Пул воркеров и pipeline собирают из тех же деталей: канал задач, N горутин, одна горутина закрывает выход после `wg.Wait()`.

---

## Что запомнить

- Закрытый канал всегда готов, nil-канал не готов никогда. Ветку выключают присваиванием `nil`.
- `time.After` в цикле — таймаут тишины, а не общий лимит.
- Пустой `default` в цикле — busy loop на 100% CPU.
- Паника в горутине без `recover` роняет весь процесс; одновременная запись в map — `fatal error`.
- `go test -race` — в CI: детектор гонок встроен в тулчейн.

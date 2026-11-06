---
title: errgroup
summary: Группа горутин с первой ошибкой и отменой — errgroup.Group, WithContext, SetLimit и TryGo вместо Task.WhenAll.
difficulty: 3
minutes: 25
prerequisites:
  - 05-concurrency/11-waitgroup
  - 05-concurrency/18-context
docs:
  - title: "Пакет golang.org/x/sync/errgroup"
    url: https://pkg.go.dev/golang.org/x/sync/errgroup
  - title: "Go Blog: Pipelines and cancellation"
    url: https://go.dev/blog/pipelines
runner:
  race: true
---

`sync.WaitGroup` умеет только ждать. Как только задачи возвращают ошибки, вокруг него вырастает обвязка: канал ошибок, `sync.Once` для первой, отмена соседей. Всё это уже собрано в `golang.org/x/sync/errgroup` — полуофициальном модуле команды Go (`x/` — вне стандартной библиотеки, но от тех же авторов).

`Group.Go(f)` запускает `func() error` в горутине, `Wait()` ждёт все и возвращает первую ненулевую ошибку. `errgroup.WithContext(ctx)` возвращает группу и производный контекст, который отменяется при первой ошибке (причина — сама ошибка, её отдаёт `context.Cause`) или когда `Wait` вернётся. `SetLimit(n)` ограничивает число одновременно работающих горутин: `Go` блокируется, пока не освободится место, а `TryGo` в этом случае сразу возвращает `false`. Панику в задаче группа не перехватывает: она, как и в любой горутине, роняет программу.

## Как это в C#

- `await Task.WhenAll(tasks)` бросает одно исключение (первой упавшей задачи по списку), а все они лежат в `AggregateException` у `task.Exception`. `Wait` возвращает только первую ошибку — остальные теряются; если нужны все, собирайте их сами в `errors.Join`.
- `Task.WhenAll` не отменяет соседей при сбое — для этого заводят связанный `CancellationTokenSource` и вызывают `Cancel` в `catch`. `errgroup.WithContext` делает это сам.
- `Parallel.ForEachAsync` с `MaxDegreeOfParallelism` — это `SetLimit` плюс цикл с `Go`. Как и у `Parallel.ForEachAsync`, при первой ошибке группа с контекстом перестаёт быть полезной: остальные задачи получают отмену.
- `errgroup` — внешний модуль: `go get golang.org/x/sync`. В песочнице курса он уже доступен.

## Ловушки

### Задачи слушают не тот контекст

```go
g, gctx := errgroup.WithContext(ctx)
for _, u := range urls {
	g.Go(func() error {
		return fetch(ctx, u) // внешний ctx: отмена группы до задач не дойдёт
	})
}
```

Отменяется только контекст, который вернул `WithContext`. Внутри задач передавайте его — `gctx` (часто его прямо называют `ctx`, затеняя внешний).

### Контекст группы после `Wait`

```go
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { return load(ctx) })
if err := g.Wait(); err != nil {
	return err
}
return save(ctx, data) // ctx уже отменён: Wait отменяет его при возврате
```

Контекст группы живёт ровно столько, сколько группа. Дальнейшую работу делайте с исходным контекстом.

### `SetLimit(0)`

```go
g.SetLimit(cfg.Workers) // Workers не задан — 0
g.Go(task)              // заблокируется навсегда
```

Лимит 0 запрещает запуск горутин, а «без лимита» — это отрицательное значение (по умолчанию лимита нет). Менять лимит, пока в группе работают горутины, нельзя — `SetLimit` запаникует.

## Задание

Напишите функцию, которая параллельно обрабатывает элементы с помощью `errgroup`:

```go
func ProcessAll(ctx context.Context, items []string, limit int,
	f func(ctx context.Context, item string) (int, error)) ([]int, error)
```

Требования:

- при успехе возвращается срез длины `len(items)`, где `out[i]` — результат `f(ctx, items[i])`; порядок совпадает с `items`;
- если `limit > 0`, одновременно выполняется не больше `limit` вызовов `f`; при `limit <= 0` ограничения нет;
- при первой ошибке `f` все остальные получают отменённый контекст (тот, что передан в `f`), `ProcessAll` возвращает `nil` и именно эту первую ошибку — а не `context.Canceled` от отменённых соседей;
- новые вызовы `f` не начинаются, как только контекст группы отменён — первой ошибкой `f` или отменой самого `ctx`: элемент, до которого дошла очередь после этого, пропускается и даёт ошибку `ctx.Err()`;
- если отменён сам `ctx`, уже запущенные `f` видят отмену через свой контекст, а `ProcessAll` возвращает `nil` и первую по времени ошибку — ту, что вернула `f`, или `ctx.Err()` пропущенного элемента (`context.Canceled` либо `context.DeadlineExceeded`); если `ctx` отменён ещё до вызова, `f` не вызывается ни разу;
- для пустого или `nil` среза `items` — пустой срез (не `nil`) и `nil`-ошибка;
- к моменту возврата все запущенные горутины завершились.

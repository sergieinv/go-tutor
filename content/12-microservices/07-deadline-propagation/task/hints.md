## Подсказка 1

`Child` — арифметика над `ctx.Deadline()`. Сначала `ctx.Err()`, потом один раз `now := b.Now()` (или `time.Now`). Таймаут по умолчанию — `Max`; если дедлайн есть — `min(dl.Sub(now)-b.Reserve, b.Max)`. Меньше `Min` — `ErrNoTime`. Дочерний контекст — `context.WithDeadline(ctx, now.Add(timeout))`: именно от `ctx`, а не от `context.Background()`, иначе отмена и значения не дойдут.

## Подсказка 2

В `Gather` поверх контекста из `Child` создайте ещё один `context.WithCancel` — его отмена и будет сигналом «один вызов упал, остальным хватит». Обе функции отмены — через `defer`: тогда после возврата контекст вызовов отменён в любом случае. Первую ошибку удобно сохранить через `sync.Once`, вызвав в нём и `stop()`.

## Подсказка 3

```go
var wg sync.WaitGroup
var once sync.Once
var first error
for _, call := range calls {
	wg.Go(func() {
		if err := call(child); err != nil {
			once.Do(func() { first = err; stop() })
		}
	})
}
wg.Wait()
return first
```

То же самое делает `errgroup.WithContext` из `golang.org/x/sync` — им тоже можно воспользоваться: `g, gctx := errgroup.WithContext(child)`, `g.Go(...)`, `return g.Wait()`.

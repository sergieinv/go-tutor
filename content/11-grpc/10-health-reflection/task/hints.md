## Подсказка 1

Разделите `Refresh` на два этапа. Сначала проверьте каждую зависимость из `deps` ровно один раз и запишите результат в `map[string]bool`. Потом пройдите по `services` и посчитайте статус каждого сервиса по этой карте: имя, которого в карте нет, — сбой. Общий статус `""` — SERVING, только если SERVING все сервисы. Статусы выставляются через `hs.SetServingStatus`.

## Подсказка 2

- Параллельно: горутина на зависимость (`sync.WaitGroup`, в Go 1.25 — `wg.Go(func() {...})`), запись в общую карту — под `sync.Mutex`.
- Таймаут: у каждой проверки свой `context.WithTimeout(ctx, timeout)` и `defer cancel()`.
- Паника в горутине роняет весь процесс, поэтому `recover` нужен внутри горутины проверки, а не снаружи `Refresh`.
- Упавшие имена соберите в множество (`map[string]bool`), затем в срез и `slices.Sort`.

## Подсказка 3

```go
func (m *Monitor) check(ctx context.Context, d Dependency) (err error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return d.Check(ctx)
}

func (m *Monitor) Run(ctx context.Context, tick <-chan time.Time) {
	m.Refresh(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-tick:
			if !ok {
				return // tick закрыт
			}
			m.Refresh(ctx)
		}
	}
}
```

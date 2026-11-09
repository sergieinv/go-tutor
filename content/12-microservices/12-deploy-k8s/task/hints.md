## Подсказка 1

Состояние — под одним `sync.Mutex`: стадия, флаг «старт завершён» (его ставит `MarkReady` и больше никто не снимает) и `map[string]func(context.Context) error` с проверками. `Drain` просто ставит `Draining`, `MarkReady` меняет стадию, только если она `Starting`. Обработчики — `http.HandlerFunc`, общий хелпер ответа ставит `Content-Type`, вызывает `WriteHeader` и `fmt.Fprintln(w, body)`.

## Подсказка 2

В `Ready` под мьютексом прочитайте стадию и скопируйте проверки, а запускайте их уже без мьютекса — иначе зависшая проверка заблокирует `AddCheck` и `Drain`. Контекст проверок — `context.WithTimeout(r.Context(), timeout)`. Каждая проверка — в своей горутине, результаты — в канал с буфером на число проверок: тогда опоздавшие горутины не зависнут на отправке, когда их уже никто не читает.

## Подсказка 3

Сбор результатов с таймаутом:

```go
pending := len(checks)
for pending > 0 {
	select {
	case res := <-results:
		pending--
		delete(waiting, res.name)
		if !res.ok {
			failed = append(failed, res.name)
		}
	case <-ctx.Done():
		for name := range waiting { // не ответили — упали
			failed = append(failed, name)
		}
		pending = 0
	}
}
slices.Sort(failed)
```

Тело — `"not ready: " + strings.Join(failed, ",")`.

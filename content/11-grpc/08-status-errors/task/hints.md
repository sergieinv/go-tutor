## Подсказка 1

`ToStatus` — один `switch` с условиями в том порядке, в каком они перечислены в задании: первое совпадение побеждает. Ошибки приходят обёрнутыми (`fmt.Errorf("...: %w", ...)`, `errors.Join`), поэтому сравнивайте через `errors.Is` для переменных-ошибок и `errors.As` для типов. Ветка `default` — `status.New(codes.Internal, "internal error")`: текст исходной ошибки в статус не попадает.

## Подсказка 2

Детали добавляет `st.WithDetails(...)` — он возвращает **новый** статус и ошибку; исходный не меняется. Сообщения деталей — из `google.golang.org/genproto/googleapis/rpc/errdetails`: `BadRequest` с `FieldViolations`, `PreconditionFailure` с `Violations`, `ErrorInfo{Reason, Domain}`, `RetryInfo{RetryDelay: durationpb.New(d)}`. В `ShouldRetry` статус достаёт `status.FromError(err)` (он разворачивает и обёрнутые ошибки), а `st.Details()` возвращает уже распакованные сообщения — перебирайте их и проверяйте тип `*errdetails.RetryInfo`.

## Подсказка 3

```go
func retryDelay(st *status.Status) (time.Duration, bool) {
	for _, d := range st.Details() {
		ri, ok := d.(*errdetails.RetryInfo)
		if !ok {
			continue
		}
		rd := ri.GetRetryDelay()
		if rd == nil || rd.CheckValid() != nil || rd.AsDuration() < 0 {
			return 0, false // первый RetryInfo негоден — считаем, что его нет
		}
		return rd.AsDuration(), true
	}
	return 0, false
}
```

Дальше: `Unavailable` и `Aborted` → `true` и задержка (или 0); `ResourceExhausted` → `true`, только если `retryDelay` вернул `true`; всё остальное → `false, 0`.

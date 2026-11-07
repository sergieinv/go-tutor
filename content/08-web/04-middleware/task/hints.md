## Подсказка 1

`Chain` оборачивает с конца списка, чтобы первая middleware оказалась снаружи:

```go
for i := len(mws) - 1; i >= 0; i-- {
	h = mws[i](h)
}
return h
```

Каждая middleware — функция, возвращающая `http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ... next.ServeHTTP(w, r) ... })`.

## Подсказка 2

Для `Logging` заведите структуру со встроенным `http.ResponseWriter` и полями `status`, `bytes`. Переопределите `WriteHeader` (запоминать только первый код) и `Write` (если статуса ещё нет — это неявный 200; прибавить `n`). Добавьте `Unwrap() http.ResponseWriter { return w.ResponseWriter }`. Если после `next.ServeHTTP` статус всё ещё 0 — хендлер ничего не записал, в журнал идёт 200.

## Подсказка 3

Ключ контекста — неэкспортируемый тип `type requestIDKey struct{}`. В `RequestID`: проверить заголовок (длина и допустимые байты), при неудаче взять `gen()`, затем `w.Header().Set(RequestIDHeader, id)` и `next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))`. `RequestIDFrom` — `id, _ := ctx.Value(requestIDKey{}).(string)`: форма с `, ok` не паникует, если значения нет.

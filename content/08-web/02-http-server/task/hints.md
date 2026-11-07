## Подсказка 1

Состояние — `sync.Mutex` и `int` в структуре: нулевые значения обоих готовы к работе, конструктор не нужен. Ресивер `ServeHTTP` — указатель, иначе каждый запрос будет работать с копией счётчика (и копией мьютекса — `go vet` об этом предупредит). Ветвление по методу — `switch r.Method` с константами `http.MethodGet` и т.д.

## Подсказка 2

Параметр запроса: `r.URL.Query()` возвращает `url.Values`. `Has("by")` отличает «параметра нет» (берём 1) от «параметр пустой» (`?by=` — ошибка). Число — `strconv.Atoi`, затем проверка диапазона. После `http.Error` — `return`, иначе код ниже допишет ответ и изменит счётчик.

## Подсказка 3

Заголовки ставятся до записи тела — удобно вынести ответ 200 в функцию:

```go
func writeValue(w http.ResponseWriter, n int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(strconv.Itoa(n) + "\n"))
}
```

Для 405 `Allow` тоже задаётся до `http.Error`: тот вызывает `WriteHeader`, и всё, что добавлено позже, в ответ не попадёт. Для `DELETE` — только `w.WriteHeader(http.StatusNoContent)`.

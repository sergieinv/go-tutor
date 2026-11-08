## Подсказка 1

Разделите ответственность. Сервис проверяет ввод (возвращает `*ValidationError`, не трогая хранилище) и оборачивает ошибки хранилища `fmt.Errorf("item %d: %w", id, err)`. Хендлер разбирает путь и тело, зовёт сервис с `r.Context()` и при любой ошибке передаёт её в одну функцию, которая выбирает статус. Ни сервис, ни хендлер не создают свой `context.Background()`.

## Подсказка 2

В функции перевода ошибки в статус порядок такой: `errors.As(err, &ve)` для `*ValidationError` → `400` с `ve.Error()`; `errors.Is` для `ErrNotFound` → `404`, `ErrConflict` → `409`, `context.DeadlineExceeded` или `context.Canceled` → `503`; всё остальное — `500` и запись в лог. `errors.Is`/`errors.As` проходят всю цепочку `%w`, поэтому обёртки сервиса им не мешают.

## Подсказка 3

```go
default:
	h.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
```

`writeJSON` ставит `Content-Type: application/json`, затем `w.WriteHeader(status)` и `json.NewEncoder(w).Encode(v)`. Маршруты: `mux.HandleFunc("GET /items/{id}", ...)` и `"POST /items/{id}/reserve"`, id — `strconv.ParseInt(r.PathValue("id"), 10, 64)`, тело — `http.MaxBytesReader` и `json.NewDecoder(r.Body).Decode(&body)`; успех резерва — `w.WriteHeader(http.StatusNoContent)`.

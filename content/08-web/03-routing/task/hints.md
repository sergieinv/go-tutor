## Подсказка 1

Таблица маршрутов переносится в `mux.HandleFunc` почти дословно: `"GET /{$}"`, `"GET admin.local/{$}"`, `"POST /notes/{$}"`, `"GET /notes/count"`, `"GET /notes/{id}"`, `"DELETE /notes/{id}"`, `"GET /raw/{path...}"`. Порядок регистрации не важен: `/notes/count` специфичнее `/notes/{id}`, а хостовый шаблон проверяется раньше остальных. 404, 405 и `Allow` писать не нужно — это сделает `ServeMux`.

## Подсказка 2

Хранилище — структура с `sync.Mutex`, `map[int]string` и счётчиком следующего id; создавайте её внутри `NewRouter`, а хендлеры сделайте её методами или замыканиями. Для списка в порядке id: `slices.Sorted(maps.Keys(notes))`.

## Подсказка 3

Проверка `{id}` без знака и ведущих нулей — разбор и обратная запись должны совпасть:

```go
s := r.PathValue("id")
id, err := strconv.Atoi(s)
if err != nil || id < 1 || strconv.Itoa(id) != s {
	http.NotFound(w, r)
	return
}
```

Тело `POST`: `io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))`; превышение лимита — ошибка типа `*http.MaxBytesError` (проверьте через `errors.As`).

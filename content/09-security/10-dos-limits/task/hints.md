## Подсказка 1

Семафор — буферизованный канал `slots := make(chan struct{}, l.MaxInFlight)`, созданный один раз в `Protect`, а не в каждом запросе. Занять место без ожидания — `select` с веткой `default`: `case slots <- struct{}{}:` — место есть, сразу `defer func() { <-slots }()`; `default:` — `503`. Так место освободится при любом выходе из обработчика, включая отказы `400` и `415`.

## Подсказка 2

Тело ограничивайте до распаковки: `r.Body = http.MaxBytesReader(w, r.Body, l.MaxBodyBytes)`. `gzip.NewReader(r.Body)` сразу читает заголовок gzip — его ошибка и есть «не gzip», ответ `400`. Распакованное тело ограничивает тот же `http.MaxBytesReader`, обёрнутый вокруг распаковщика: он принимает любой `io.ReadCloser`, возвращает `*http.MaxBytesError` и пропускает ровно `max` байт.

## Подсказка 3

```go
zr, err := gzip.NewReader(r.Body)
if err != nil {
	http.Error(w, "bad gzip", http.StatusBadRequest)
	return
}
defer zr.Close()
r.Body = http.MaxBytesReader(w, zr, l.MaxDecompressed)
r.Header.Del("Content-Encoding")
r.ContentLength = -1
```

`NewServer` — литерал `&http.Server{...}` с пятью полями из `l` и `Handler: Protect(h, l)`.

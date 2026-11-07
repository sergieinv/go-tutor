## Подсказка 1

Запрос с контекстом — `http.NewRequestWithContext(ctx, http.MethodGet, url, nil)`, заголовок — `req.Header.Set("Accept", "application/json")`. Ошибки `NewRequestWithContext` и `client.Do` оборачивайте через `fmt.Errorf("getjson: %w", err)`: `*url.Error` из `Do` уже содержит ошибку контекста, `%w` сохранит цепочку для `errors.Is`. Сразу после проверки ошибки `Do` — `defer resp.Body.Close()`.

## Подсказка 2

Чтобы тело было дочитано при любом исходе, прочитайте его **до** проверок статуса и типа: `io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))`. Тело в пределах лимита будет прочитано до `EOF`, а `len(body) > maxBytes` означает превышение. Порядок ошибок задаёт условие, а не порядок чтения: сначала статус, потом тип, потом размер.

## Подсказка 3

Медиатип без параметров и в нижнем регистре даёт `mime.ParseMediaType`:

```go
mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
if err != nil || mt != "application/json" {
	return ErrNotJSON
}
```

Статус проверяйте как `resp.StatusCode < 200 || resp.StatusCode > 299` и возвращайте `&StatusError{StatusCode: resp.StatusCode}`. В конце — `json.Unmarshal(body, v)`: он сам отвергнет данные после первого значения.

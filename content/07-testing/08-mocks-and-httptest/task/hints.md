## Подсказка 1

Клиент — это `http.NewRequestWithContext(ctx, http.MethodGet, url, nil)`, `req.Header.Set("Accept", "application/json")` и `c.HTTP.Do(req)`. Сразу после успешного `Do` — `defer resp.Body.Close()`. Ошибку `Do` верните как есть: это `*url.Error`, и `errors.Is(err, context.DeadlineExceeded)` сквозь него работает. Путь соберите как `c.BaseURL + "/users/" + url.PathEscape(login)`, а лишний `/` в конце `BaseURL` уберите ещё в `NewClient` через `strings.TrimSuffix`.

## Подсказка 2

Статусы разберите `switch resp.StatusCode`: для 404 — `fmt.Errorf("%s: %w", login, ErrNotFound)`, для всего, кроме 200, — `fmt.Errorf("users: статус %d", resp.StatusCode)`. Тело разбирайте только при 200: `json.NewDecoder(resp.Body).Decode(&u)`.

## Подсказка 3

Кеш — `map[string]entry` под `sync.Mutex`, где `entry` хранит пользователя и время получения:

```go
c.mu.Lock()
e, ok := c.entries[login]
c.mu.Unlock()
if ok && c.now().Sub(e.at) < c.ttl {
	return e.user, nil
}
u, err := c.src.User(ctx, login)
if err != nil {
	return User{}, err // ошибку не кешируем
}
c.mu.Lock()
c.entries[login] = entry{u, c.now()}
c.mu.Unlock()
return u, nil
```

Map создайте в `NewCache` — запись в nil-map паникует. Держать блокировку на время вызова `src` тоже корректно, просто медленный запрос задержит всех.

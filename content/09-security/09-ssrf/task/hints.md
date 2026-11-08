## Подсказка 1

Не пытайтесь разбирать URL: тесты атакуют внутренний сервер через `localhost`, `[::ffff:127.0.0.1]`, `0.0.0.0` и редирект с разрешённого сервера. Ищите место, через которое проходит *каждое* соединение клиента и где уже известен IP. Оно в `net.Dialer`, который транспорт использует для подключения.

## Подсказка 2

`net.Dialer{ControlContext: func(ctx context.Context, network, address string, c syscall.RawConn) error {...}}` вызывается перед каждым `connect`, `address` — уже разрешённый `"IP:порт"`. Разберите его через `netip.ParseAddrPort`, сделайте `Unmap()` адресу, спросите `allow` и при отказе верните `fmt.Errorf("%w: ...", ErrBlocked)`: ошибка дойдёт до `client.Get` обёрнутой, и `errors.Is` её найдёт. Передайте `d.DialContext` в `http.Transport{DialContext: ..., Proxy: nil}`. Для `IsPublic` хватит методов `netip.Addr` и двух `netip.Prefix`.

## Подсказка 3

```go
return &http.Client{
	Timeout:   15 * time.Second,
	Transport: &http.Transport{Proxy: nil, DialContext: d.DialContext},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// via — уже сделанные запросы: на первом редиректе len(via) == 1
		if len(via) > 3 {
			return errors.New("слишком много редиректов")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("схема запрещена")
		}
		return nil
	},
}
```

`IsPublic`: `a = a.Unmap()`, затем `a.IsGlobalUnicast() && !a.IsPrivate()` и два `!prefix.Contains(a)`.

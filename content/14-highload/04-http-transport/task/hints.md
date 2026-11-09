## Подсказка 1

Начните `NewClient` с копии стандартного транспорта: `tr := http.DefaultTransport.(*http.Transport).Clone()` — в нём уже есть прокси из окружения, `IdleConnTimeout`, `TLSHandshakeTimeout` и HTTP/2. Поменяйте `MaxConnsPerHost` и `MaxIdleConnsPerHost` (оба — `n`), задайте `DialContext` от `&net.Dialer{Timeout: ..., KeepAlive: ...}` и не забудьте про `n < 1` и `timeout <= 0`. `Clone` каждый раз создаёт новый транспорт — общий пул между клиентами не появится.

## Подсказка 2

В `GetJSON` после успешного `c.Do` сначала проверьте `resp.ContentLength > MaxBody` — тогда просто `Close` и `ErrTooLarge`. Во всех остальных случаях повесьте один `defer`, который дочитывает остаток и закрывает тело: `io.Copy(io.Discard, io.LimitReader(resp.Body, MaxBody))`, потом `resp.Body.Close()`. Так дочитывание сработает и после ошибки статуса, и после ошибки декодирования, и после удачного `Decode`.

## Подсказка 3

Ошибку `Do` оборачивайте через `%w` (`fmt.Errorf("get %s: %w", url, err)`) — тогда `errors.Is(err, context.DeadlineExceeded)` найдёт и таймаут клиента, и дедлайн контекста. Статус проверяйте до декодирования: `if resp.StatusCode < 200 || resp.StatusCode > 299 { return &StatusError{Code: resp.StatusCode} }`. Декодируйте из ограниченного потока: `json.NewDecoder(io.LimitReader(resp.Body, MaxBody)).Decode(dst)`.

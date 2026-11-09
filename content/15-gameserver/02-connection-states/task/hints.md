## Подсказка 1

Разделите `Handle` на обёртку и внутренний метод: обёртка проверяет `StateClosed`, вызывает `handle`, а при ошибке ставит `StateClosed` и возвращает `nil, err`. Тогда правило «ошибка закрывает соединение» записано один раз. Внутри — `switch s.state`, а в каждой ветке — type switch по пакету (`switch p := p.(type)`); всё, что не вернуло результат, проваливается в `return nil, ErrUnexpectedPacket` в конце.

## Подсказка 2

Для keep-alive хватит трёх полей: `lastKA time.Time` (когда отправлен последний keep-alive, а при входе — время входа), `nextID int64` и `pending int64` (0 — не ждём). `Tick`: если `pending != 0` — проверить таймаут `now.Sub(s.lastKA) >= Timeout`; иначе, если прошёл интервал, `nextID++`, `pending = nextID`, `lastKA = now`. Ту же проверку таймаута сделайте в начале `handle` для состояния play.

## Подсказка 3

JSON соберите `encoding/json` из анонимной структуры с тегами — без `omitempty`, чтобы нули попали в ответ:

```go
var st struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	} `json:"players"`
	Description struct {
		Text string `json:"text"`
	} `json:"description"`
}
```

Ник проверяйте по байтам (`'a'..'z'`, `'A'..'Z'`, `'0'..'9'`, `'_'`), чат — `utf8.ValidString` и цикл `for _, r := range text` с `unicode.IsControl(r)`.

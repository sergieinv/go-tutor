## Подсказка 1

Идите в три прохода по одной структуре: заполнить значения по умолчанию, применить окружение (запоминая ошибки разбора в срез), применить флаги через `flag.NewFlagSet(..., flag.ContinueOnError)` — флаг по умолчанию берёт текущее значение поля, так что переопределяет только явно заданное. Потом проверить итог и вернуть все проблемы разом через `errors.Join`.

## Подсказка 2

Метод `Is` у `*FieldError` уже написан: `errors.Join` из таких ошибок сам по себе даёт `errors.Is(err, ErrInvalidConfig)`. Ошибку разбора флагов оборачивайте `fmt.Errorf("%w: %w", ErrInvalidConfig, err)`, а `flag.ErrHelp` возвращайте как есть. Позиционные аргументы остаются в `fs.Args()` — их наличие тоже ошибка. Не забудьте `fs.SetOutput(io.Discard)`, иначе `flag` напечатает справку в stderr.

## Подсказка 3

- адрес: `_, port, err := net.SplitHostPort(addr)`, затем `strconv.Atoi(port)` и диапазон `0..65535`;
- DSN: `u, err := url.Parse(dsn)`, верен, если `err == nil`, схема `postgres` или `postgresql` и `u.Host != ""`; в `Problem` — описание, но не значение;
- `String`: `url.Parse` + `u.Redacted()` для postgres-URL (параметр `password` перед этим замените через `q := u.Query()`, `q.Set`, `u.RawQuery = q.Encode()`), иначе `***`; остальное — `fmt.Sprintf("addr=%s dsn=%s max_conns=%d read_timeout=%s debug=%t", ...)`.

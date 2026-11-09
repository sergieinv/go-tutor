## Подсказка 1

Порядок проверок: достать `event-id` (первый заголовок с таким ключом; нет или пусто — сразу в DLQ с `ErrNoEventID` и попытками `0`), затем `Store.Seen` (дубль — `Duplicate`), затем попытки. Отправку в DLQ вынесите в отдельный метод `toDLQ(ctx, r, cause, attempts) error` — она нужна в двух местах.

## Подсказка 2

Цикл попыток со счётчиком:

```go
attempts := 0
for attempts < maxAttempts {
	if attempts > 0 && ctx.Err() != nil {
		return 0, fmt.Errorf("остановка: %w", ctx.Err())
	}
	attempts++
	if err = p.Handle(ctx, r); err == nil || IsPermanent(err) {
		break
	}
}
```

После цикла `err == nil` — успех, иначе в `err` последняя ошибка, а в `attempts` — сколько раз вызван `Handle`.

## Подсказка 3

Заголовки DLQ собирайте в **новый** срез: `make([]kgo.RecordHeader, 0, len(r.Headers)+5)`, затем `append` исходных и пяти своих (`strconv.Itoa`, `strconv.FormatInt`). `append(r.Headers, ...)` при запасе ёмкости запишет в массив исходной записи. Ошибку отправки берите из `p.DLQ.ProduceSync(ctx, d).FirstErr()` и оборачивайте через `%w`; `MarkSeen` — только после успешной отправки.

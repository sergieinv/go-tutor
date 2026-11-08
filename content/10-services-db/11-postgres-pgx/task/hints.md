## Подсказка 1

`Classify`: `var pgErr *pgconn.PgError; if !errors.As(err, &pgErr) { return err }` (импорт `github.com/jackc/pgx/v5/pgconn`). Дальше — `switch pgErr.Code` или map «код → доменная ошибка». Неизвестный код — тот же `err`. Чтобы сработали и `errors.Is(res, ErrDuplicate)`, и `errors.As(res, &pgErr)`, оберните двумя `%w`: `fmt.Errorf("%w: %w", ErrDuplicate, err)`.

## Подсказка 2

`BuildUpsert`: сначала проверки, потом сборка. Идентификатор проверяется регулярным выражением `^[a-z_][a-z0-9_]{0,62}$` (63 символа — лимит имени в PostgreSQL). Повторы ищутся через `map[string]bool`, принадлежность списку — `slices.Contains`. Не забудьте, что колонка из `update` не может быть в `conflict`.

## Подсказка 3

Сборка через `strings.Builder`: имена — в двойных кавычках (`"` + id + `"`), значения — `$1..$N` по порядку `cols`. Хвост:

```go
if len(update) == 0 {
	b.WriteString(`ON CONFLICT ("sku") DO NOTHING`) // ключ — из conflict
} else {
	// "price" = EXCLUDED."price", ... через ", "
}
```

Между частями — ровно один пробел, без точки с запятой в конце.

## Подсказка 1

Миграции goose — обычные файлы `NNNNN_name.sql` с разделами `-- +goose Up` и `-- +goose Down`; в `fstest.MapFS` ключ — имя файла, значение — `&fstest.MapFile{Data: []byte(...)}` (в литерале map можно просто `{Data: ...}`). `Down` перечисляет обратные действия в обратном порядке: сначала индекс, потом таблица. Столбец удаляется так: `ALTER TABLE customers DROP COLUMN email`.

## Подсказка 2

Всё делает провайдер: `p, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)`. Список миграций из файлов — `p.ListSources()` (у каждой `Version`), текущая версия базы — `p.GetDBVersion(ctx)`. Дальше — `p.UpTo(ctx, target)` или `p.DownTo(ctx, target)`: оба возвращают `[]*goose.MigrationResult`, версия — `r.Source.Version`. `Pending` — `p.Status(ctx)` и `s.State == goose.StatePending`.

## Подсказка 3

Цель проверяйте **до** `GetDBVersion`: первый запрос провайдера создаёт таблицу `goose_db_version`, а неверная цель не должна трогать базу. При сбое `UpTo` возвращает `nil` и ошибку `*goose.PartialError`, а применённые до сбоя миграции лежат в её поле `Applied`:

```go
var partial *goose.PartialError
if errors.As(err, &partial) {
	res = partial.Applied
}
return versions(res), fmt.Errorf("migrate к версии %d: %w", target, err)
```

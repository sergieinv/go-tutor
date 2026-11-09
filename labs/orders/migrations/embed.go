// Package migrations хранит схему базы для PostgreSQL (основная) и SQLite (тесты, запуск без Docker).
//
// Файлы — миграции goose (секции «-- +goose Up» / «-- +goose Down»),
// они же — схема для sqlc (см. sqlc.yaml): sqlc учитывает только секции Up.
// Миграции встроены в бинарник через embed: образу не нужны файлы рядом,
// а версия схемы всегда совпадает с версией кода.
package migrations

import "embed"

// FS содержит каталоги postgres и sqlite. Хранилище берёт свой
// подкаталог через fs.Sub.
//
//go:embed postgres/*.sql sqlite/*.sql
var FS embed.FS

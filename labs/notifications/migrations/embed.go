// Package migrations хранит схему базы «Уведомлений» — миграции goose
// (секции «-- +goose Up» / «-- +goose Down»). Они встроены в бинарник через
// embed: образу не нужны файлы рядом, а версия схемы всегда совпадает с
// версией кода.
package migrations

import "embed"

// FS содержит файлы миграций SQLite.
//
//go:embed *.sql
var FS embed.FS

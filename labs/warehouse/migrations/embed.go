// Package migrations — схема базы «Склада» (MySQL), миграции goose.
//
// Файлы встроены в бинарник через embed: образу не нужны файлы рядом, а
// версия схемы всегда совпадает с версией кода.
package migrations

import "embed"

// FS — миграции goose (секции «-- +goose Up» / «-- +goose Down»).
//
//go:embed *.sql
var FS embed.FS

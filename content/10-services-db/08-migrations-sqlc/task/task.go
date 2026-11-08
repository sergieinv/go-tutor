package task

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"testing/fstest"
)

// ErrBadTarget — запрошенной версии нет среди миграций.
var ErrBadTarget = errors.New("migrate: неизвестная версия")

// Migrations — миграции схемы в формате goose.
var Migrations = fstest.MapFS{
	"00001_customers.sql": {Data: []byte(`-- +goose Up
CREATE TABLE customers (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

-- +goose Down
DROP TABLE customers;
`)},
	// TODO: 00002_orders.sql и 00003_customer_email.sql
}

// Migrate приводит схему к версии target и возвращает версии
// выполненных миграций в порядке выполнения.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS, target int64) ([]int64, error) {
	// TODO: goose.NewProvider(goose.DialectSQLite3, db, fsys), UpTo / DownTo
	return nil, errors.New("не реализовано")
}

// Pending возвращает версии ещё не применённых миграций по возрастанию.
func Pending(ctx context.Context, db *sql.DB, fsys fs.FS) ([]int64, error) {
	// TODO: Provider.Status
	return nil, errors.New("не реализовано")
}

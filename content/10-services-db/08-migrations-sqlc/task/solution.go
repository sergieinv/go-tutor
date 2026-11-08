package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing/fstest"

	"github.com/pressly/goose/v3"
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
	"00002_orders.sql": {Data: []byte(`-- +goose Up
CREATE TABLE orders (
    id          INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers (id),
    total       INTEGER NOT NULL CHECK (total >= 0)
);
CREATE INDEX orders_customer_idx ON orders (customer_id);

-- +goose Down
DROP INDEX orders_customer_idx;
DROP TABLE orders;
`)},
	"00003_customer_email.sql": {Data: []byte(`-- +goose Up
ALTER TABLE customers ADD COLUMN email TEXT;

-- +goose Down
ALTER TABLE customers DROP COLUMN email;
`)},
}

// Migrate приводит схему к версии target и возвращает версии
// выполненных миграций в порядке выполнения.
func Migrate(ctx context.Context, db *sql.DB, fsys fs.FS, target int64) ([]int64, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// Цель проверяем до обращения к базе: список миграций
	// провайдер собрал из файлов.
	known := target == 0
	for _, s := range p.ListSources() {
		known = known || s.Version == target
	}
	if !known {
		return nil, fmt.Errorf("%w: %d", ErrBadTarget, target)
	}
	current, err := p.GetDBVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: версия схемы: %w", err)
	}
	var res []*goose.MigrationResult
	switch {
	case target > current:
		res, err = p.UpTo(ctx, target)
	case target < current:
		res, err = p.DownTo(ctx, target)
	}
	if err != nil {
		// Миграции до сбоя уже применены — их список лежит
		// в *goose.PartialError.
		var partial *goose.PartialError
		if errors.As(err, &partial) {
			res = partial.Applied
		}
		return versions(res), fmt.Errorf("migrate к версии %d: %w", target, err)
	}
	return versions(res), nil
}

// Pending возвращает версии ещё не применённых миграций по возрастанию.
func Pending(ctx context.Context, db *sql.DB, fsys fs.FS) ([]int64, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return nil, fmt.Errorf("pending: %w", err)
	}
	status, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("pending: %w", err)
	}
	pending := []int64{}
	for _, s := range status {
		if s.State == goose.StatePending {
			pending = append(pending, s.Source.Version)
		}
	}
	slices.Sort(pending)
	return pending, nil
}

func versions(res []*goose.MigrationResult) []int64 {
	out := make([]int64, 0, len(res))
	for _, r := range res {
		out = append(out, r.Source.Version)
	}
	return out
}

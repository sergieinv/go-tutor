package task

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

func sqlFile(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// columns возвращает имена столбцов таблицы по порядку.
func columns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols
}

// userTables — все таблицы базы, кроме служебных таблиц SQLite.
func userTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	return names
}

func TestOrdersSchema(t *testing.T) {
	db := openDB(t)
	mustMigrate(t, db, 2, []int64{1, 2})
	if got, want := columns(t, db, "orders"), []string{"id", "customer_id", "total"}; !slices.Equal(got, want) {
		t.Fatalf("столбцы orders = %v, want %v", got, want)
	}
	if _, err := db.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Анна')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO orders (customer_id, total) VALUES (1, 100)`); err != nil {
		t.Fatalf("корректный заказ не вставился: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO orders (customer_id, total) VALUES (99, 100)`); err == nil {
		t.Error("заказ несуществующего покупателя вставился: нет внешнего ключа на customers(id)")
	}
	if _, err := db.Exec(`INSERT INTO orders (customer_id, total) VALUES (1, -5)`); err == nil {
		t.Error("заказ с total < 0 вставился: нет CHECK (total >= 0)")
	}
	if _, err := db.Exec(`INSERT INTO orders (customer_id) VALUES (1)`); err == nil {
		t.Error("заказ без total вставился: total должен быть NOT NULL")
	}
	var idx string
	err := db.QueryRow(`SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = 'orders_customer_idx'`).Scan(&idx)
	if err != nil || idx != "orders" {
		t.Errorf("индекс orders_customer_idx на orders: %q, %v", idx, err)
	}
	var col string
	err = db.QueryRow(`SELECT name FROM pragma_index_info('orders_customer_idx')`).Scan(&col)
	if err != nil || col != "customer_id" {
		t.Errorf("индекс orders_customer_idx должен быть по customer_id: %q, %v", col, err)
	}
	mustMigrate(t, db, 1, []int64{2})
	if tableExists(t, db, "orders") {
		t.Error("Down миграции 2 не удалил orders")
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'orders_customer_idx'`).Scan(&n)
	if n != 0 {
		t.Error("Down миграции 2 не удалил индекс orders_customer_idx")
	}
}

func TestCustomerEmail(t *testing.T) {
	db := openDB(t)
	mustMigrate(t, db, 3, []int64{1, 2, 3})
	if got, want := columns(t, db, "customers"), []string{"id", "name", "email"}; !slices.Equal(got, want) {
		t.Fatalf("столбцы customers = %v, want %v", got, want)
	}
	// Шаг expand: старый код, не знающий про email, продолжает
	// вставлять строки — столбец допускает NULL.
	if _, err := db.Exec(`INSERT INTO customers (name) VALUES ('старый код')`); err != nil {
		t.Fatalf("вставка без email: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO customers (name, email) VALUES ('новый код', 'n@x.io')`); err != nil {
		t.Fatalf("вставка с email: %v", err)
	}
	mustMigrate(t, db, 2, []int64{3})
	if got, want := columns(t, db, "customers"), []string{"id", "name"}; !slices.Equal(got, want) {
		t.Fatalf("после отката 3 столбцы customers = %v, want %v", got, want)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM customers`).Scan(&n)
	if n != 2 {
		t.Fatalf("после отката 3 в customers %d строк, want 2", n)
	}
}

func TestDownToZero(t *testing.T) {
	db := openDB(t)
	mustMigrate(t, db, 3, []int64{1, 2, 3})
	mustMigrate(t, db, 0, []int64{3, 2, 1})
	if got := userTables(t, db); !slices.Equal(got, []string{"goose_db_version"}) {
		t.Fatalf("после отката до 0 таблицы: %v, want только goose_db_version", got)
	}
	mustPending(t, db, []int64{1, 2, 3})
	mustMigrate(t, db, 0, nil)
}

func TestPartialUp(t *testing.T) {
	db := openDB(t)
	mustMigrate(t, db, 1, []int64{1})
	mustPending(t, db, []int64{2, 3})
	mustMigrate(t, db, 2, []int64{2})
	mustPending(t, db, []int64{3})
}

func TestBadTargetUntouched(t *testing.T) {
	gapped := fstest.MapFS{
		"00001_a.sql":  sqlFile("-- +goose Up\nCREATE TABLE a (x INTEGER);\n-- +goose Down\nDROP TABLE a;\n"),
		"00002_b.sql":  sqlFile("-- +goose Up\nCREATE TABLE b (x INTEGER);\n-- +goose Down\nDROP TABLE b;\n"),
		"00010_c.sql":  sqlFile("-- +goose Up\nCREATE TABLE c (x INTEGER);\n-- +goose Down\nDROP TABLE c;\n"),
		"readme.txt":   sqlFile("не миграция"),
		"helpers.sql":  sqlFile("SELECT 1;"),
		"00003_d.yaml": sqlFile("не миграция"),
	}
	for _, target := range []int64{-1, 5, 11, 3} {
		db := openDB(t)
		got, err := Migrate(context.Background(), db, gapped, target)
		if !errors.Is(err, ErrBadTarget) {
			t.Errorf("Migrate(%d) = (%v, %v), want ErrBadTarget", target, got, err)
		}
		if len(got) != 0 {
			t.Errorf("Migrate(%d) при неверной цели вернул %v, want пустой срез", target, got)
		}
		if tables := userTables(t, db); len(tables) != 0 {
			t.Errorf("Migrate(%d) с неверной целью тронул базу: таблицы %v", target, tables)
		}
	}
	db := openDB(t)
	got, err := Migrate(context.Background(), db, gapped, 10)
	if err != nil || !slices.Equal(got, []int64{1, 2, 10}) {
		t.Fatalf("Migrate(10) = (%v, %v), want ([1 2 10], nil)", got, err)
	}
	got, err = Migrate(context.Background(), db, gapped, 2)
	if err != nil || !slices.Equal(got, []int64{10}) {
		t.Fatalf("Migrate(2) = (%v, %v), want ([10], nil)", got, err)
	}
}

func TestBrokenMigration(t *testing.T) {
	broken := fstest.MapFS{
		"00001_a.sql": sqlFile("-- +goose Up\nCREATE TABLE a (x INTEGER);\n-- +goose Down\nDROP TABLE a;\n"),
		"00002_b.sql": sqlFile("-- +goose Up\nCREATE TABLE b (x INTEGER);\n-- +goose Down\nDROP TABLE b;\n"),
		"00003_c.sql": sqlFile("-- +goose Up\nCREATE TABLE c (x INTEGER);\nINSERT INTO missing VALUES (1);\n-- +goose Down\nDROP TABLE c;\n"),
	}
	db := openDB(t)
	got, err := Migrate(context.Background(), db, broken, 3)
	if err == nil {
		t.Fatal("Migrate со сломанной миграцией вернул nil")
	}
	if errors.Is(err, ErrBadTarget) {
		t.Fatalf("сломанная миграция — не ErrBadTarget: %v", err)
	}
	var partial *goose.PartialError
	if !errors.As(err, &partial) {
		t.Errorf("ошибка goose должна быть обёрнута через %%w: errors.As(*goose.PartialError) = false для %v", err)
	}
	if !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("Migrate вернул %v, want [1 2] — миграции, применённые до сбоя", got)
	}
	// Сломанная миграция откатилась целиком: goose выполняет
	// каждую в транзакции.
	if tableExists(t, db, "c") {
		t.Error("таблица c из сломанной миграции осталась")
	}
	pending, err := Pending(context.Background(), db, broken)
	if err != nil || !slices.Equal(pending, []int64{3}) {
		t.Errorf("Pending = (%v, %v), want ([3], nil)", pending, err)
	}
}

func TestPendingFreshDB(t *testing.T) {
	db := openDB(t)
	fsys := fstest.MapFS{
		"00005_b.sql": sqlFile("-- +goose Up\nCREATE TABLE b (x INTEGER);\n"),
		"00002_a.sql": sqlFile("-- +goose Up\nCREATE TABLE a (x INTEGER);\n"),
	}
	got, err := Pending(context.Background(), db, fsys)
	if err != nil || !slices.Equal(got, []int64{2, 5}) {
		t.Fatalf("Pending = (%v, %v), want ([2 5], nil)", got, err)
	}
}

func TestCanceledContext(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Migrate(ctx, db, Migrations, 3); err == nil {
		t.Error("Migrate с отменённым контекстом вернул nil")
	}
	if tables := userTables(t, db); slices.Contains(tables, "customers") {
		t.Error("Migrate с отменённым контекстом применил миграции")
	}
}

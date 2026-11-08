package task

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	_ "modernc.org/sqlite"
)

// openDB открывает пустую базу во временном каталоге теста
// с включённой проверкой внешних ключей.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db") + "?_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// tableExists сообщает, есть ли в базе таблица name.
func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func mustMigrate(t *testing.T, db *sql.DB, target int64, want []int64) {
	t.Helper()
	got, err := Migrate(context.Background(), db, Migrations, target)
	if err != nil {
		t.Fatalf("Migrate(%d) = %v", target, err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Migrate(%d) = %v, want %v", target, got, want)
	}
}

func mustPending(t *testing.T, db *sql.DB, want []int64) {
	t.Helper()
	got, err := Pending(context.Background(), db, Migrations)
	if err != nil {
		t.Fatalf("Pending = %v", err)
	}
	if !slices.Equal(got, want) { // nil и пустой срез равны
		t.Fatalf("Pending = %v, want %v", got, want)
	}
}

func TestMigrateUpAll(t *testing.T) {
	db := openDB(t)
	mustPending(t, db, []int64{1, 2, 3})
	mustMigrate(t, db, 3, []int64{1, 2, 3})
	mustPending(t, db, nil)
	for _, table := range []string{"customers", "orders"} {
		if !tableExists(t, db, table) {
			t.Errorf("после Migrate(3) нет таблицы %s", table)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db := openDB(t)
	mustMigrate(t, db, 3, []int64{1, 2, 3})
	if _, err := db.Exec(`INSERT INTO customers (name, email) VALUES ('Анна', 'anna@x.io')`); err != nil {
		t.Fatal(err)
	}
	// Повторный вызов ничего не выполняет и не трогает данные.
	mustMigrate(t, db, 3, nil)
	var n int
	db.QueryRow(`SELECT count(*) FROM customers`).Scan(&n)
	if n != 1 {
		t.Fatalf("после повторного Migrate в customers %d строк, want 1", n)
	}
}

func TestMigrateDownAndUp(t *testing.T) {
	db := openDB(t)
	mustMigrate(t, db, 3, []int64{1, 2, 3})
	if _, err := db.Exec(`INSERT INTO customers (name) VALUES ('Борис')`); err != nil {
		t.Fatal(err)
	}
	mustMigrate(t, db, 1, []int64{3, 2})
	if tableExists(t, db, "orders") {
		t.Error("после отката до 1 таблица orders осталась")
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM customers`).Scan(&name); err != nil || name != "Борис" {
		t.Errorf("после отката до 1 customers: %q, %v", name, err)
	}
	mustPending(t, db, []int64{2, 3})
	mustMigrate(t, db, 3, []int64{2, 3})
}

func TestBadTarget(t *testing.T) {
	db := openDB(t)
	if _, err := Migrate(context.Background(), db, Migrations, 7); !errors.Is(err, ErrBadTarget) {
		t.Fatalf("Migrate(7) = %v, want ErrBadTarget", err)
	}
}

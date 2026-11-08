package mysql_test

import (
	"crypto/rand"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"gotutor/labs/orders/internal/orders"
	ordersmysql "gotutor/labs/orders/internal/store/mysql"
	"gotutor/labs/orders/internal/storetest"
)

// TestStore запускает контрактный набор против настоящего MySQL.
//
// ORDERS_TEST_MYSQL — DSN драйвера с правами создавать базы, например
// root:labs@tcp(127.0.0.1:53306)/. Каждый подтест получает свою базу
// orders_test_<случайное> и удаляет её.
func TestStore(t *testing.T) {
	adminDSN := os.Getenv("ORDERS_TEST_MYSQL")
	if adminDSN == "" {
		t.Skip("ORDERS_TEST_MYSQL is not set")
	}
	storetest.Run(t, func(t *testing.T) orders.Store {
		return newTestStore(t, adminDSN)
	})
}

func newTestStore(t *testing.T, adminDSN string) *ordersmysql.Store {
	t.Helper()
	ctx := t.Context()

	cfg, err := mysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal("ORDERS_TEST_MYSQL is not a valid DSN")
	}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}

	// Имя базы — идентификатор, его не передать через «?». Строим из
	// безопасных символов и берём в обратные кавычки.
	name := "orders_test_" + strings.ToLower(rand.Text())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		admin.Close()
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := storetest.CleanupContext()
		defer cancel()
		defer admin.Close()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	cfg.DBName = name
	s, err := ordersmysql.Open(ctx, cfg.FormatDSN())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close) // выполнится раньше DROP DATABASE: t.Cleanup — LIFO
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

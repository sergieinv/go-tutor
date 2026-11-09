package mysql_test

import (
	"crypto/rand"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/prometheus/client_golang/prometheus"

	"gotutor/labs/warehouse/internal/stock"
	whmysql "gotutor/labs/warehouse/internal/store/mysql"
	"gotutor/labs/warehouse/internal/storetest"
)

// TestStore запускает контрактный набор против настоящего MySQL.
//
// WAREHOUSE_TEST_MYSQL — DSN с правами создавать базы, например
// root:labs@tcp(127.0.0.1:53306)/. Каждый подтест получает свою базу
// warehouse_test_<случайное> и удаляет её.
func TestStore(t *testing.T) {
	adminDSN := os.Getenv("WAREHOUSE_TEST_MYSQL")
	if adminDSN == "" {
		t.Skip("WAREHOUSE_TEST_MYSQL is not set")
	}
	storetest.Run(t, func(t *testing.T, levels map[int64]int32) stock.Store {
		return newTestStore(t, adminDSN, levels)
	})
}

func newTestStore(t *testing.T, adminDSN string, levels map[int64]int32) *whmysql.Store {
	t.Helper()
	ctx := t.Context()

	cfg, err := mysql.ParseDSN(adminDSN)
	if err != nil {
		t.Fatal("WAREHOUSE_TEST_MYSQL is not a valid DSN")
	}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}

	// Имя базы — идентификатор, его не передать через «?». Строим из
	// безопасных символов и берём в обратные кавычки.
	name := "warehouse_test_" + strings.ToLower(rand.Text())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4"); err != nil {
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
	// Свой реестр на каждую базу: метрика повторов регистрируется в Open.
	s, err := whmysql.Open(ctx, cfg.FormatDSN(), prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() }) // раньше DROP DATABASE: t.Cleanup — LIFO
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Остатки — прямой вставкой: у хранилища нет API «завести товар»,
	// склад получает товары миграциями и (в проде) из приёмки.
	for id, n := range levels {
		if _, err := admin.ExecContext(ctx,
			"INSERT INTO `"+name+"`.stock (product_id, available, reserved) VALUES (?, ?, 0) AS new "+
				"ON DUPLICATE KEY UPDATE available = new.available, reserved = 0", id, n); err != nil {
			t.Fatalf("seed stock: %v", err)
		}
	}
	return s
}

// TestMigrateTwice: повторный Migrate на актуальной схеме — не ошибка
// (рестарт сервиса), демо-остатки на месте.
func TestMigrateTwice(t *testing.T) {
	adminDSN := os.Getenv("WAREHOUSE_TEST_MYSQL")
	if adminDSN == "" {
		t.Skip("WAREHOUSE_TEST_MYSQL is not set")
	}
	s := newTestStore(t, adminDSN, nil)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	l, err := s.Level(t.Context(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if l != (stock.Level{ProductID: 3, Available: 3}) {
		t.Errorf("demo stock of product 3 = %+v, want 3 available", l)
	}
}

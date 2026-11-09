package postgres_test

import (
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"gotutor/labs/orders/internal/store/postgres"
	"gotutor/labs/orders/internal/storetest"
)

// TestStore запускает контрактный набор против настоящего PostgreSQL.
//
// ORDERS_TEST_POSTGRES — URL с правами создавать базы, например
// postgres://postgres:labs@127.0.0.1:55432/postgres?sslmode=disable.
// Каждый подтест получает свою базу orders_test_<случайное> и удаляет её.
func TestStore(t *testing.T) {
	adminDSN := os.Getenv("ORDERS_TEST_POSTGRES")
	if adminDSN == "" {
		t.Skip("ORDERS_TEST_POSTGRES is not set")
	}
	storetest.Run(t, func(t *testing.T) storetest.Store {
		return newTestStore(t, adminDSN)
	})
}

func newTestStore(t *testing.T, adminDSN string) *postgres.Store {
	t.Helper()
	ctx := t.Context()

	u, err := url.Parse(adminDSN)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("ORDERS_TEST_POSTGRES must be a postgres:// URL")
	}
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}

	// Имя базы нельзя передать плейсхолдером ($1 — только для значений).
	// Поэтому оно строится из безопасных символов и всё равно экранируется.
	name := "orders_test_" + strings.ToLower(rand.Text())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		admin.Close(ctx)
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := storetest.CleanupContext()
		defer cancel()
		defer admin.Close(ctx)
		// WITH (FORCE) обрывает соединения, если кто-то их не закрыл.
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	u.Path = "/" + name
	s, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close) // выполнится раньше DROP DATABASE: t.Cleanup — LIFO
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
}

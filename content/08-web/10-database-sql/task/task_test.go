package task

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// newRepo открывает пустую базу во временном каталоге теста.
func newRepo(t *testing.T) (*Repo, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	return NewRepo(db), db
}

func ptr(s string) *string { return &s }

func TestCreateGet(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	id, err := repo.Create(ctx, "Анна", ptr("anna@x.io"), 100)
	if err != nil || id != 1 {
		t.Fatalf("Create = (%d, %v), want (1, nil)", id, err)
	}
	a, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get = %v", err)
	}
	if a.ID != 1 || a.Owner != "Анна" || a.Email == nil || *a.Email != "anna@x.io" || a.Balance != 100 {
		t.Fatalf("Get = %+v", a)
	}
}

func TestGetNotFound(t *testing.T) {
	repo, _ := newRepo(t)
	if _, err := repo.Get(context.Background(), 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get несуществующего = %v, want ErrNotFound", err)
	}
}

func TestTransfer(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	a, _ := repo.Create(ctx, "a", nil, 100)
	b, _ := repo.Create(ctx, "b", nil, 0)
	if err := repo.Transfer(ctx, a, b, 30); err != nil {
		t.Fatalf("Transfer = %v", err)
	}
	if err := repo.Transfer(ctx, a, b, 100); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Transfer сверх баланса = %v, want ErrInsufficientFunds", err)
	}
	list, err := repo.List(ctx, 0)
	if err != nil || len(list) != 2 || list[0].Balance != 70 || list[1].Balance != 30 {
		t.Fatalf("List = (%+v, %v), want балансы 70 и 30", list, err)
	}
}

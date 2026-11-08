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

// newDB — база в файле во временном каталоге теста. maxOpen > 0 ограничивает
// пул; 0 — без ограничения (так утечка соединений не мешает функциональным
// проверкам). WAL — чтобы незакрытое чтение не блокировало запись.
func newDB(t *testing.T, maxOpen int) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "shop.db") + "?_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(maxOpen)
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	for _, it := range []Item{
		{1, "чай", "drink", "A1"},
		{2, "кофе", "drink", "A2"},
		{3, "печенье", "food", "B1"},
		{4, "какао", "drink", "A3"},
		{5, "сахар", "food", "B2"},
	} {
		if _, err := db.Exec(`INSERT INTO items (id, name, tag, shelf) VALUES (?, ?, ?, ?)`, it.ID, it.Name, it.Tag, it.Shelf); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func ids(items []Item) []int64 {
	var out []int64
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestByTag(t *testing.T) {
	tests := []struct {
		tag   string
		limit int
		want  []int64
	}{
		{"drink", 10, []int64{1, 2, 4}},
		{"drink", 3, []int64{1, 2, 4}},
		{"drink", 2, []int64{1, 2}},
		{"food", 1, []int64{3}},
		{"toys", 5, nil},
	}
	for _, tt := range tests {
		// Свежая база на каждый случай.
		repo := NewRepo(newDB(t, 0))
		got, err := repo.ByTag(context.Background(), tt.tag, tt.limit)
		if err != nil {
			t.Fatalf("ByTag(%q, %d): %v", tt.tag, tt.limit, err)
		}
		if !slices.Equal(ids(got), tt.want) {
			t.Errorf("ByTag(%q, %d) = id %v, want %v", tt.tag, tt.limit, ids(got), tt.want)
		}
	}
	got, _ := NewRepo(newDB(t, 0)).ByTag(context.Background(), "drink", 1)
	if len(got) != 1 || got[0] != (Item{1, "чай", "drink", "A1"}) {
		t.Errorf("ByTag(drink, 1) = %+v, want [{1 чай drink A1}]", got)
	}
}

func TestByTagBadLimit(t *testing.T) {
	repo := NewRepo(newDB(t, 0))
	for _, limit := range []int{0, -1} {
		if _, err := repo.ByTag(context.Background(), "drink", limit); !errors.Is(err, ErrBadLimit) {
			t.Errorf("ByTag(drink, %d) err = %v, want ErrBadLimit", limit, err)
		}
	}
}

func TestCount(t *testing.T) {
	repo := NewRepo(newDB(t, 0))
	for tag, want := range map[string]int{"drink": 3, "food": 2, "toys": 0} {
		if got, err := repo.Count(context.Background(), tag); err != nil || got != want {
			t.Errorf("Count(%q) = %d, %v, want %d, nil", tag, got, err, want)
		}
	}
}

func TestDelete(t *testing.T) {
	db := newDB(t, 0)
	repo := NewRepo(db)
	ctx := context.Background()
	if err := repo.Delete(ctx, 2); err != nil {
		t.Fatalf("Delete(2): %v", err)
	}
	if err := repo.Delete(ctx, 42); err != nil {
		t.Fatalf("Delete(42) несуществующего: %v, want nil", err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM items`).Scan(&n)
	if n != 4 {
		t.Fatalf("после Delete(2) товаров %d, want 4", n)
	}
}

func TestMove(t *testing.T) {
	db := newDB(t, 0)
	repo := NewRepo(db)
	if err := repo.Move(context.Background(), 3, "C7"); err != nil {
		t.Fatalf("Move(3, C7): %v", err)
	}
	var shelf string
	db.QueryRow(`SELECT shelf FROM items WHERE id = 3`).Scan(&shelf)
	var item int64
	var logged string
	db.QueryRow(`SELECT item_id, shelf FROM moves`).Scan(&item, &logged)
	if shelf != "C7" || item != 3 || logged != "C7" {
		t.Fatalf("после Move(3, C7): полка %q, журнал (%d, %q), want C7 и (3, C7)", shelf, item, logged)
	}
}

func TestMoveErrors(t *testing.T) {
	db := newDB(t, 0)
	repo := NewRepo(db)
	ctx := context.Background()
	if err := repo.Move(ctx, 42, "C7"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Move(42, C7) err = %v, want ErrNotFound", err)
	}
	if err := repo.Move(ctx, 1, ""); !errors.Is(err, ErrBadShelf) {
		t.Errorf("Move(1, \"\") err = %v, want ErrBadShelf", err)
	}
	var shelf string
	var moves int
	db.QueryRow(`SELECT shelf FROM items WHERE id = 1`).Scan(&shelf)
	db.QueryRow(`SELECT count(*) FROM moves`).Scan(&moves)
	if shelf != "A1" || moves != 0 {
		t.Errorf("после ошибок Move: полка товара 1 %q, записей в журнале %d, want A1 и 0", shelf, moves)
	}
}

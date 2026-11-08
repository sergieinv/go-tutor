package task

import (
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// Фабрики хранилищ: каждый вызов — новое пустое хранилище.
func newSQLite(t *testing.T) Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // у каждого соединения с :memory: своя база
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	return NewSQLiteStore(db)
}

func newMem(t *testing.T) Store { return NewMemStore() }

// testStore — контракт хранилища. Его проходит SQLiteStore; фейк
// обязан проходить его тоже.
func testStore(t *testing.T, newStore func(t *testing.T) Store) {
	t.Run("create and get", func(t *testing.T) {
		s := newStore(t)
		id, err := s.Create(t.Context(), "лампа", 3)
		if id != 1 || err != nil {
			t.Fatalf("Create(лампа, 3) = (%d, %v), want (1, nil)", id, err)
		}
		if it, err := s.Get(t.Context(), 1); it != (Item{1, "лампа", 3}) || err != nil {
			t.Errorf("Get(1) = (%+v, %v), want ({1 лампа 3}, nil)", it, err)
		}
	})
	t.Run("ids are sequential", func(t *testing.T) {
		s := newStore(t)
		for i, name := range []string{"a", "b", "c"} {
			if id, err := s.Create(t.Context(), name, 0); id != int64(i+1) || err != nil {
				t.Errorf("Create(%s) = (%d, %v), want (%d, nil)", name, id, err, i+1)
			}
		}
	})
	t.Run("reserve", func(t *testing.T) {
		s := newStore(t)
		id, _ := s.Create(t.Context(), "лампа", 3)
		if err := s.Reserve(t.Context(), id, 2); err != nil {
			t.Fatalf("Reserve(2) = %v", err)
		}
		if err := s.Reserve(t.Context(), id, 2); !errors.Is(err, ErrInsufficient) {
			t.Errorf("Reserve(2) при остатке 1 = %v, want ErrInsufficient", err)
		}
		if it, _ := s.Get(t.Context(), id); it.Qty != 1 {
			t.Errorf("остаток = %d, want 1", it.Qty)
		}
	})
	t.Run("not found", func(t *testing.T) {
		s := newStore(t)
		if it, err := s.Get(t.Context(), 42); !errors.Is(err, ErrNotFound) || it != (Item{}) {
			t.Errorf("Get(42) = (%+v, %v), want (Item{}, ErrNotFound)", it, err)
		}
		if err := s.Reserve(t.Context(), 42, 1); !errors.Is(err, ErrNotFound) {
			t.Errorf("Reserve(42, 1) = %v, want ErrNotFound", err)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		s := newStore(t)
		s.Create(t.Context(), "лампа", 1)
		if _, err := s.Create(t.Context(), "лампа", 5); !errors.Is(err, ErrDuplicate) {
			t.Errorf("второй Create(лампа) = %v, want ErrDuplicate", err)
		}
	})
	t.Run("list", func(t *testing.T) {
		s := newStore(t)
		s.Create(t.Context(), "b", 2)
		s.Create(t.Context(), "a", 1)
		items, err := s.List(t.Context())
		want := []Item{{1, "b", 2}, {2, "a", 1}}
		if err != nil || len(items) != 2 || items[0] != want[0] || items[1] != want[1] {
			t.Errorf("List = (%+v, %v), want (%+v, nil)", items, err, want)
		}
	})
}

func TestSQLiteStore(t *testing.T) { testStore(t, newSQLite) }

func TestMemStore(t *testing.T) { testStore(t, newMem) }

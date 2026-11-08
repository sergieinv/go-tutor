package task

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// testStoreEdges — краевые случаи контракта.
func testStoreEdges(t *testing.T, newStore func(t *testing.T) Store) {
	t.Run("invalid create stores nothing", func(t *testing.T) {
		s := newStore(t)
		for _, c := range []struct {
			name string
			qty  int
		}{{"", 1}, {"x", -1}, {"", -1}} {
			if id, err := s.Create(t.Context(), c.name, c.qty); !errors.Is(err, ErrInvalid) || id != 0 {
				t.Errorf("Create(%q, %d) = (%d, %v), want (0, ErrInvalid)", c.name, c.qty, id, err)
			}
		}
		if id, err := s.Create(t.Context(), "x", 0); id != 1 || err != nil {
			t.Errorf("Create(x, 0) после неудачных = (%d, %v), want (1, nil): неудачные не тратят id", id, err)
		}
	})
	t.Run("duplicate keeps original and ids", func(t *testing.T) {
		s := newStore(t)
		s.Create(t.Context(), "лампа", 1)
		s.Create(t.Context(), "лампа", 5)
		if id, err := s.Create(t.Context(), "Лампа", 2); id != 2 || err != nil {
			t.Errorf("Create(Лампа) = (%d, %v), want (2, nil): имена сравниваются с учётом регистра, дубль не тратит id", id, err)
		}
		if it, _ := s.Get(t.Context(), 1); it.Qty != 1 {
			t.Errorf("Get(1).Qty = %d, want 1: дубль не меняет существующий товар", it.Qty)
		}
	})
	t.Run("invalid reserve", func(t *testing.T) {
		s := newStore(t)
		id, _ := s.Create(t.Context(), "лампа", 3)
		for _, q := range []int{0, -1} {
			if err := s.Reserve(t.Context(), id, q); !errors.Is(err, ErrInvalid) {
				t.Errorf("Reserve(%d) = %v, want ErrInvalid", q, err)
			}
		}
		if err := s.Reserve(t.Context(), 99, 0); !errors.Is(err, ErrInvalid) {
			t.Errorf("Reserve(99, 0) = %v, want ErrInvalid (проверка qty раньше поиска)", err)
		}
		if it, _ := s.Get(t.Context(), id); it.Qty != 3 {
			t.Errorf("остаток после неверных Reserve = %d, want 3", it.Qty)
		}
		if err := s.Reserve(t.Context(), id, 3); err != nil {
			t.Errorf("Reserve всего остатка = %v", err)
		}
	})
	t.Run("empty list", func(t *testing.T) {
		s := newStore(t)
		if items, err := s.List(t.Context()); len(items) != 0 || err != nil {
			t.Errorf("List пустого = (%+v, %v), want (len 0, nil)", items, err)
		}
	})
	t.Run("list is a copy and sorted", func(t *testing.T) {
		s := newStore(t)
		for _, n := range []string{"e", "d", "c", "b", "a", "f", "g", "h"} {
			s.Create(t.Context(), n, 1)
		}
		items, _ := s.List(t.Context())
		for i, it := range items {
			if it.ID != int64(i+1) {
				t.Fatalf("List: на позиции %d id %d — нужен порядок по id", i, it.ID)
			}
		}
		if len(items) > 0 {
			items[0].Qty = 100
		}
		if it, _ := s.Get(t.Context(), 1); it.Qty != 1 {
			t.Errorf("изменение результата List поменяло хранилище: Qty = %d", it.Qty)
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		s := newStore(t)
		id, _ := s.Create(t.Context(), "лампа", 3)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := s.Create(ctx, "стол", 1); !errors.Is(err, context.Canceled) {
			t.Errorf("Create = %v, want context.Canceled", err)
		}
		if _, err := s.Create(ctx, "", -1); !errors.Is(err, context.Canceled) {
			t.Errorf("Create с неверными аргументами = %v, want context.Canceled (отмена проверяется первой)", err)
		}
		if _, err := s.Get(ctx, id); !errors.Is(err, context.Canceled) {
			t.Errorf("Get = %v, want context.Canceled", err)
		}
		if err := s.Reserve(ctx, id, 1); !errors.Is(err, context.Canceled) {
			t.Errorf("Reserve = %v, want context.Canceled", err)
		}
		if _, err := s.List(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("List = %v, want context.Canceled", err)
		}
		if items, _ := s.List(t.Context()); len(items) != 1 || items[0].Qty != 3 {
			t.Errorf("после вызовов с отменённым контекстом: %+v, want только лампа с остатком 3", items)
		}
	})
	t.Run("concurrent reserve", func(t *testing.T) {
		s := newStore(t)
		id, _ := s.Create(t.Context(), "лампа", 50)
		var mu sync.Mutex
		ok, short := 0, 0
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				for range 5 {
					err := s.Reserve(t.Context(), id, 1)
					mu.Lock()
					switch {
					case err == nil:
						ok++
					case errors.Is(err, ErrInsufficient):
						short++
					default:
						t.Errorf("Reserve = %v", err)
					}
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		it, _ := s.Get(t.Context(), id)
		if ok != 50 || short != 50 || it.Qty != 0 {
			t.Errorf("100 конкурентных Reserve(1) при остатке 50: успешных %d, ErrInsufficient %d, остаток %d; want 50, 50, 0", ok, short, it.Qty)
		}
	})
}

func TestSQLiteStoreEdges(t *testing.T) { testStoreEdges(t, newSQLite) }

func TestMemStoreEdges(t *testing.T) { testStoreEdges(t, newMem) }

func TestMemStoreInstancesIndependent(t *testing.T) {
	a, b := NewMemStore(), NewMemStore()
	a.Create(t.Context(), "x", 1)
	if id, err := b.Create(t.Context(), "x", 1); id != 1 || err != nil {
		t.Errorf("второе хранилище: Create(x) = (%d, %v), want (1, nil) — у экземпляров не должно быть общего состояния", id, err)
	}
}

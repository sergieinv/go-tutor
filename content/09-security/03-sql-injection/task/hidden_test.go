package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

// assertIntact проверяет, что атака не изменила таблицу.
func assertIntact(t *testing.T, db *sql.DB) {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM products`).Scan(&n); err != nil || n != len(seed) {
		t.Fatalf("после атаки в products %d строк (ошибка %v), want %d", n, err, len(seed))
	}
}

func TestExploitNameInjection(t *testing.T) {
	db := newDB(t)
	// Строка со «взломом» — просто подстрока имени.
	search(t, db, Filter{NameContains: "' OR 1=1 --"}, 3)
	search(t, db, Filter{NameContains: "x' OR 'a'='a"})
	search(t, db, Filter{NameContains: "%' OR 1=1 --", Sort: "price"})
	search(t, db, Filter{NameContains: "'; DELETE FROM products; --"})
	assertIntact(t, db)
}

func TestExploitCategoryInjection(t *testing.T) {
	db := newDB(t)
	search(t, db, Filter{Categories: []string{"books", "x') OR 1=1 --"}}, 2, 5, 8)
	search(t, db, Filter{Categories: []string{"books' OR category <> '"}})
	assertIntact(t, db)
}

func TestExploitSortInjection(t *testing.T) {
	db := newDB(t)
	for _, s := range []string{
		"price; DROP TABLE products",
		"(CASE WHEN (SELECT count(*) FROM products) > 0 THEN name ELSE price END)",
		"price--",
		"name DESC",
		"PRICE",
		" price",
		"id, (SELECT 1)",
	} {
		got, err := Search(context.Background(), db, Filter{Sort: s})
		if !errors.Is(err, ErrBadSort) || got != nil {
			t.Errorf("Sort %q: (%d товаров, %v), want (nil, ErrBadSort)", s, len(got), err)
		}
	}
	assertIntact(t, db)
}

func TestExploitLikeWildcards(t *testing.T) {
	// % и _ во вводе — обычные символы, а не «что угодно».
	db := newDB(t)
	search(t, db, Filter{NameContains: "%"}, 4)
	search(t, db, Filter{NameContains: "_"}, 5)
	search(t, db, Filter{NameContains: `\`}, 6)
	search(t, db, Filter{NameContains: "%%"})
	search(t, db, Filter{NameContains: "C_ble"})
	search(t, db, Filter{NameContains: `\s`}, 6)
}

func TestSortByName(t *testing.T) {
	db := newDB(t)
	// Порядок SQLite по умолчанию — побайтовый: цифры, заглавные, строчные, кириллица.
	search(t, db, Filter{Sort: "name"}, 4, 6, 7, 2, 8, 3, 1, 5, 9)
	search(t, db, Filter{Sort: "-name"}, 9, 5, 1, 3, 8, 2, 7, 6, 4)
	search(t, db, Filter{Sort: "price"}, 7, 1, 3, 9, 6, 5, 4, 2, 8)
	search(t, db, Filter{Sort: "id", Categories: []string{"mugs"}}, 3, 9)
}

func TestCaseAndUnicode(t *testing.T) {
	db := newDB(t)
	search(t, db, Filter{NameContains: "USB cable"}, 1)
	search(t, db, Filter{NameContains: "gopher"}, 9)
	search(t, db, Filter{NameContains: "Кружка"}, 9)
	search(t, db, Filter{NameContains: " "}, 1, 2, 3, 4, 5, 6, 7, 8, 9)
}

func TestCategoriesEdgeCases(t *testing.T) {
	db := newDB(t)
	search(t, db, Filter{Categories: []string{}}, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	search(t, db, Filter{Categories: []string{"books", "books"}}, 2, 5, 8)
	search(t, db, Filter{Categories: []string{"Books"}})
	search(t, db, Filter{Categories: []string{""}})
	many := []string{"posters"}
	for i := range 100 {
		many = append(many, fmt.Sprintf("cat-%d", i))
	}
	search(t, db, Filter{Categories: many}, 6)
}

func TestConnectionReleased(t *testing.T) {
	// Пул из одного соединения: незакрытые rows заблокируют следующий запрос.
	db := newDB(t)
	for range 5 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := Search(ctx, db, Filter{Categories: []string{"books"}, Sort: "-price"})
		cancel()
		if err != nil {
			t.Fatalf("Search: %v (rows не закрыты?)", err)
		}
	}
}

func TestCanceledContext(t *testing.T) {
	db := newDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Search(ctx, db, Filter{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Search с отменённым ctx: %v, want context.Canceled", err)
	}
}

func TestDBError(t *testing.T) {
	db := newDB(t)
	if _, err := db.Exec(`DROP TABLE products`); err != nil {
		t.Fatal(err)
	}
	if got, err := Search(context.Background(), db, Filter{}); err == nil {
		t.Fatalf("Search без таблицы: (%v, nil), want ошибку", got)
	}
}

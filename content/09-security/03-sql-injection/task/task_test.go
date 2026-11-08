package task

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	_ "modernc.org/sqlite"
)

var seed = []Product{
	{1, "USB Cable 2m", "cables", 500},
	{2, "Go Programming", "books", 3000},
	{3, "Mug ' OR 1=1 -- edition", "mugs", 700},
	{4, "100% Cotton Shirt", "clothes", 1500},
	{5, "snake_case Book", "books", 1200},
	{6, `Back\slash Poster`, "posters", 900},
	{7, "Cable Organizer", "cables", 300},
	{8, "Learning SQL", "books", 3000},
	{9, "Кружка Gopher", "mugs", 700},
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1) // у каждого соединения с :memory: своя база
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	for _, p := range seed {
		if _, err := db.Exec(`INSERT INTO products (id, name, category, price) VALUES (?, ?, ?, ?)`, p.ID, p.Name, p.Category, p.Price); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func ids(ps []Product) []int64 {
	out := []int64{}
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

// search вызывает Search и сравнивает id результата с want.
func search(t *testing.T, db *sql.DB, f Filter, want ...int64) {
	t.Helper()
	got, err := Search(context.Background(), db, f)
	if err != nil {
		t.Errorf("Search(%+v): ошибка %v", f, err)
		return
	}
	if want == nil {
		want = []int64{}
	}
	if !slices.Equal(ids(got), want) {
		t.Errorf("Search(%+v) вернул id %v, want %v", f, ids(got), want)
	}
}

func TestSearchAll(t *testing.T) {
	db := newDB(t)
	got, err := Search(context.Background(), db, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, seed) {
		t.Fatalf("пустой фильтр:\n got %+v\nwant %+v", got, seed)
	}
}

func TestSearchFilters(t *testing.T) {
	db := newDB(t)
	search(t, db, Filter{Categories: []string{"books"}}, 2, 5, 8)
	search(t, db, Filter{Categories: []string{"mugs", "cables"}}, 1, 3, 7, 9)
	search(t, db, Filter{NameContains: "cable"}, 1, 7)
	search(t, db, Filter{NameContains: "g", Categories: []string{"books"}, Sort: "-price"}, 2, 8)
	search(t, db, Filter{NameContains: "нет такого"})
}

func TestSearchSort(t *testing.T) {
	db := newDB(t)
	search(t, db, Filter{Sort: "-price"}, 2, 8, 4, 5, 6, 3, 9, 1, 7)
	search(t, db, Filter{Sort: "-id", Categories: []string{"books"}}, 8, 5, 2)
}

func TestBadSort(t *testing.T) {
	db := newDB(t)
	for _, s := range []string{"category", "price desc", "+price"} {
		got, err := Search(context.Background(), db, Filter{Sort: s})
		if !errors.Is(err, ErrBadSort) || got != nil {
			t.Errorf("Sort %q: (%v, %v), want (nil, ErrBadSort)", s, got, err)
		}
	}
}

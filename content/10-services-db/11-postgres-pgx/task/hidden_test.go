package task

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyOtherCodes(t *testing.T) {
	for _, code := range []string{"23502", "23514", "42P01", "08006", "40000", ""} {
		src := &pgconn.PgError{Code: code}
		if err := Classify(src); err != error(src) {
			t.Errorf("Classify(%q) = %v, want тот же err без изменений", code, err)
		}
	}
}

func TestClassifyKeepsChain(t *testing.T) {
	src := &pgconn.PgError{Code: "23505"}
	wrapped := errors.Join(errors.New("batch item 3"), src)
	err := Classify(wrapped)
	if !errors.Is(err, ErrDuplicate) || !errors.Is(err, wrapped) {
		t.Fatalf("Classify потерял доменную или исходную ошибку: %v", err)
	}
	if errors.Is(err, ErrRetry) || errors.Is(err, ErrForeignKey) {
		t.Fatalf("Classify(23505) совпал с чужой доменной ошибкой: %v", err)
	}
}

func TestBuildUpsertSingleColumn(t *testing.T) {
	got, err := BuildUpsert("events", []string{"id"}, []string{"id"}, []string{})
	want := `INSERT INTO "events" ("id") VALUES ($1) ON CONFLICT ("id") DO NOTHING`
	if err != nil || got != want {
		t.Fatalf("BuildUpsert = (%q, %v)\nwant %q", got, err, want)
	}
}

func TestBuildUpsertOrder(t *testing.T) {
	got, err := BuildUpsert("t", []string{"c", "a", "b"}, []string{"a"}, []string{"c", "b"})
	want := `INSERT INTO "t" ("c", "a", "b") VALUES ($1, $2, $3) ON CONFLICT ("a") DO UPDATE SET "c" = EXCLUDED."c", "b" = EXCLUDED."b"`
	if err != nil || got != want {
		t.Fatalf("BuildUpsert = (%q, %v)\nwant %q", got, err, want)
	}
}

func TestBuildUpsertInvalid(t *testing.T) {
	long := strings.Repeat("a", 64)
	cases := []struct {
		name                   string
		table                  string
		cols, conflict, update []string
	}{
		{"нет колонок", "t", nil, []string{"a"}, nil},
		{"нет ключа", "t", []string{"a"}, nil, nil},
		{"пустая таблица", "", []string{"a"}, []string{"a"}, nil},
		{"таблица со схемой", "public.t", []string{"a"}, []string{"a"}, nil},
		{"заглавные", "T", []string{"a"}, []string{"a"}, nil},
		{"цифра в начале", "t", []string{"1a"}, []string{"1a"}, nil},
		{"кавычка", "t", []string{"a", `b"`}, []string{"a"}, nil},
		{"пробел", "t", []string{"a", "b c"}, []string{"a"}, nil},
		{"длиннее 63", "t", []string{"a", long}, []string{"a"}, nil},
		{"кириллица", "t", []string{"a", "цена"}, []string{"a"}, nil},
		{"повтор колонки", "t", []string{"a", "a"}, []string{"a"}, nil},
		{"повтор в ключе", "t", []string{"a", "b"}, []string{"a", "a"}, nil},
		{"повтор в update", "t", []string{"a", "b"}, []string{"a"}, []string{"b", "b"}},
		{"ключ не вставляется", "t", []string{"a"}, []string{"b"}, nil},
		{"update не вставляется", "t", []string{"a"}, []string{"a"}, []string{"b"}},
		{"update ключа", "t", []string{"a", "b"}, []string{"a"}, []string{"a"}},
		{"инъекция в update", "t", []string{"a", "b"}, []string{"a"}, []string{"b = 0; --"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := BuildUpsert(c.table, c.cols, c.conflict, c.update)
			if !errors.Is(err, ErrInvalidUpsert) || got != "" {
				t.Fatalf("BuildUpsert = (%q, %v), want (\"\", ErrInvalidUpsert)", got, err)
			}
		})
	}
	got, err := BuildUpsert("t", []string{"_a", "b_2", strings.Repeat("z", 63)}, []string{"_a"}, nil)
	if err != nil || got == "" {
		t.Fatalf("допустимые идентификаторы отклонены: (%q, %v)", got, err)
	}
}

// TestPostgres — на настоящей базе профиля labs: тот же код против
// настоящих кодов ошибок PostgreSQL. Без POSTGRES_DSN пропускается.
func TestPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN не задан — нужен профиль labs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustExec(`DROP TABLE IF EXISTS reviews, items`)
	mustExec(`CREATE TABLE items (sku text PRIMARY KEY, name text NOT NULL, price int NOT NULL)`)
	mustExec(`CREATE TABLE reviews (id int PRIMARY KEY, sku text NOT NULL REFERENCES items)`)

	up, err := BuildUpsert("items", []string{"sku", "name", "price"}, []string{"sku"}, []string{"price"})
	if err != nil || up == "" {
		t.Fatalf("BuildUpsert = (%q, %v)", up, err)
	}
	mustExec(up, "mug", "Кружка", 450)
	mustExec(up, "mug", "Чашка", 500)
	var name string
	var price int
	if err := conn.QueryRow(ctx, `SELECT name, price FROM items WHERE sku = 'mug'`).Scan(&name, &price); err != nil {
		t.Fatal(err)
	}
	if name != "Кружка" || price != 500 {
		t.Fatalf("после двух upsert: (%s, %d), want (Кружка, 500)", name, price)
	}

	nothing, err := BuildUpsert("items", []string{"sku", "name", "price"}, []string{"sku"}, nil)
	if err != nil || nothing == "" {
		t.Fatalf("BuildUpsert = (%q, %v)", nothing, err)
	}
	tag, err := conn.Exec(ctx, nothing, "mug", "Стакан", 1)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("DO NOTHING: (%v, %v), want 0 строк", tag, err)
	}

	_, err = conn.Exec(ctx, `INSERT INTO items VALUES ($1, $2, $3)`, "mug", "Дубль", 1)
	if err = Classify(err); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("дубликат: %v, want ErrDuplicate", err)
	}
	_, err = conn.Exec(ctx, `INSERT INTO reviews VALUES ($1, $2)`, 1, "nope")
	if err = Classify(err); !errors.Is(err, ErrForeignKey) {
		t.Fatalf("внешний ключ: %v, want ErrForeignKey", err)
	}
	mustExec(`SET statement_timeout = '50ms'`)
	_, err = conn.Exec(ctx, `SELECT pg_sleep(2)`)
	if err = Classify(err); !errors.Is(err, ErrCanceled) {
		t.Fatalf("statement_timeout: %v, want ErrCanceled", err)
	}
}

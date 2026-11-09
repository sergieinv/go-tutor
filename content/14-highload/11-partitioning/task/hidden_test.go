package task

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Границы считаются в UTC, даже если время пришло в другом поясе.
func TestMonthlyTimeZones(t *testing.T) {
	tokyo := time.FixedZone("UTC+9", 9*3600)
	// 1 марта 05:00 в Токио — это 28 февраля 20:00 UTC.
	from := time.Date(2026, 3, 1, 5, 0, 0, 0, tokyo)
	to := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	parts, err := Monthly("orders", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(parts); !equal(got, []string{"orders_2026_02", "orders_2026_03"}) {
		t.Fatalf("имена = %v, want [orders_2026_02 orders_2026_03]", got)
	}
	for _, p := range parts {
		if p.From.Location() != time.UTC || p.To.Location() != time.UTC {
			t.Fatalf("границы %v — %v не в UTC", p.From, p.To)
		}
	}
	sql, err := parts[0].CreateSQL()
	if err != nil || !strings.Contains(sql, "FROM ('2026-02-01 00:00:00+00') TO ('2026-03-01 00:00:00+00')") {
		t.Fatalf("CreateSQL = %q, %v", sql, err)
	}
}

func TestMonthlyEdges(t *testing.T) {
	// to ровно на начале месяца — этот месяц не нужен.
	parts, err := Monthly("orders", utc(2026, 1, 1, 0), utc(2026, 2, 1, 0))
	if err != nil || !equal(names(parts), []string{"orders_2026_01"}) {
		t.Fatalf("январь целиком = %v, %v", names(parts), err)
	}
	// Одна наносекунда следующего месяца — уже нужен.
	parts, err = Monthly("orders", utc(2026, 1, 31, 23), utc(2026, 2, 1, 0).Add(time.Nanosecond))
	if err != nil || !equal(names(parts), []string{"orders_2026_01", "orders_2026_02"}) {
		t.Fatalf("через границу = %v, %v", names(parts), err)
	}
	// Через год.
	parts, err = Monthly("t_1", utc(2025, 11, 10, 0), utc(2026, 2, 2, 0))
	if err != nil || !equal(names(parts), []string{"t_1_2025_11", "t_1_2025_12", "t_1_2026_01", "t_1_2026_02"}) {
		t.Fatalf("через год = %v, %v", names(parts), err)
	}
	for i := 1; i < len(parts); i++ {
		if !parts[i].From.Equal(parts[i-1].To) {
			t.Fatalf("партиции %d и %d не стыкуются", i-1, i)
		}
	}
}

func TestBadRange(t *testing.T) {
	cases := [][2]time.Time{
		{utc(2026, 2, 1, 0), utc(2026, 2, 1, 0)},
		{utc(2026, 3, 1, 0), utc(2026, 2, 1, 0)},
		{time.Date(999, 12, 1, 0, 0, 0, 0, time.UTC), utc(1000, 2, 1, 0)},
		{utc(9999, 12, 1, 0), time.Date(10000, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		if parts, err := Monthly("orders", c[0], c[1]); !errors.Is(err, ErrBadRange) || parts != nil {
			t.Errorf("Monthly(%v, %v) = %v, %v; want nil, ErrBadRange", c[0], c[1], names(parts), err)
		}
	}
}

// Имя таблицы попадает в DDL без плейсхолдеров — только белый
// список символов.
func TestBadNames(t *testing.T) {
	long := strings.Repeat("a", 56)
	bad := []string{"", "Orders", "1orders", "orders-x", "orders; DROP TABLE users", `"orders"`, "заказы", "orders ", long}
	for _, name := range bad {
		if parts, err := Monthly(name, utc(2026, 1, 1, 0), utc(2026, 2, 1, 0)); !errors.Is(err, ErrBadName) || parts != nil {
			t.Errorf("Monthly(%q) = %v, %v; want nil, ErrBadName", name, names(parts), err)
		}
	}
	if _, err := Monthly(strings.Repeat("a", 55), utc(2026, 1, 1, 0), utc(2026, 2, 1, 0)); err != nil {
		t.Errorf("имя из 55 символов допустимо: %v", err)
	}
	if _, err := Monthly("_x9", utc(2026, 1, 1, 0), utc(2026, 2, 1, 0)); err != nil {
		t.Errorf("«_x9» допустимо: %v", err)
	}
	evil := []Partition{
		{Parent: "orders; DROP TABLE users", Name: "orders_2026_01", From: utc(2026, 1, 1, 0), To: utc(2026, 2, 1, 0)},
		{Parent: "orders", Name: "x') TO ('2030-01-01'); DROP TABLE users; --", From: utc(2026, 1, 1, 0), To: utc(2026, 2, 1, 0)},
		{Parent: "orders", Name: strings.Repeat("a", 64), From: utc(2026, 1, 1, 0), To: utc(2026, 2, 1, 0)},
	}
	for _, p := range evil {
		if sql, err := p.CreateSQL(); !errors.Is(err, ErrBadName) || sql != "" {
			t.Errorf("CreateSQL(%q, %q) = %q, %v; want ErrBadName", p.Parent, p.Name, sql, err)
		}
		if sql, err := p.DetachSQL(); !errors.Is(err, ErrBadName) || sql != "" {
			t.Errorf("DetachSQL(%q, %q) = %q, %v; want ErrBadName", p.Parent, p.Name, sql, err)
		}
	}
	ok := Partition{Parent: "orders", Name: strings.Repeat("a", 63), From: utc(2026, 1, 1, 0), To: utc(2026, 2, 1, 0)}
	if _, err := ok.CreateSQL(); err != nil {
		t.Errorf("имя партиции из 63 символов допустимо: %v", err)
	}
	empty := Partition{Parent: "orders", Name: "orders_x", From: utc(2026, 2, 1, 0), To: utc(2026, 2, 1, 0)}
	if sql, err := empty.CreateSQL(); !errors.Is(err, ErrBadRange) || sql != "" {
		t.Errorf("пустой диапазон: %q, %v; want ErrBadRange", sql, err)
	}
}

// Поля, заданные не в UTC, печатаются в UTC.
func TestCreateSQLConvertsToUTC(t *testing.T) {
	msk := time.FixedZone("UTC+3", 3*3600)
	p := Partition{Parent: "events", Name: "events_x",
		From: time.Date(2026, 1, 1, 3, 0, 0, 0, msk), To: time.Date(2026, 1, 2, 3, 30, 15, 0, msk)}
	got, err := p.CreateSQL()
	want := "CREATE TABLE IF NOT EXISTS events_x PARTITION OF events FOR VALUES FROM ('2026-01-01 00:00:00+00') TO ('2026-01-02 00:30:15+00')"
	if err != nil || got != want {
		t.Fatalf("CreateSQL = %q, %v\nwant %q", got, err, want)
	}
}

func TestLocateEdges(t *testing.T) {
	parts, _ := Monthly("orders", utc(2026, 1, 1, 0), utc(2026, 4, 1, 0))
	cases := []struct {
		t    time.Time
		want string
	}{
		{utc(2026, 1, 1, 0), "orders_2026_01"},
		{utc(2026, 2, 1, 0).Add(-time.Nanosecond), "orders_2026_01"},
		{time.Date(2026, 3, 1, 8, 59, 0, 0, time.FixedZone("UTC+9", 9*3600)), "orders_2026_02"},
		{utc(2026, 3, 31, 23), "orders_2026_03"},
		{utc(2026, 4, 1, 0), ""},
		{utc(2025, 12, 31, 23), ""},
	}
	for _, c := range cases {
		p, ok := Locate(parts, c.t)
		if c.want == "" {
			if ok {
				t.Errorf("Locate(%v) = %q, want ничего", c.t, p.Name)
			}
			continue
		}
		if !ok || p.Name != c.want {
			t.Errorf("Locate(%v) = %q, %v; want %q", c.t, p.Name, ok, c.want)
		}
	}
	if _, ok := Locate(nil, utc(2026, 1, 1, 0)); ok {
		t.Error("Locate(nil) нашёл партицию")
	}
}

func TestExpiredEdges(t *testing.T) {
	parts, _ := Monthly("orders", utc(2026, 1, 1, 0), utc(2026, 6, 1, 0)) // январь–май
	cases := []struct {
		now  time.Time
		keep int
		want []string
	}{
		{utc(2026, 5, 1, 0), 0, []string{"orders_2026_01", "orders_2026_02", "orders_2026_03", "orders_2026_04"}},
		{utc(2026, 5, 31, 23), -3, []string{"orders_2026_01", "orders_2026_02", "orders_2026_03", "orders_2026_04"}},
		{utc(2026, 3, 1, 0), 2, []string{}},
		{utc(2026, 4, 1, 0), 2, []string{"orders_2026_01"}},
		{utc(2027, 1, 1, 0), 1, []string{"orders_2026_01", "orders_2026_02", "orders_2026_03", "orders_2026_04", "orders_2026_05"}},
		// 1 мая 02:00 по Москве — ещё 30 апреля в UTC.
		{time.Date(2026, 5, 1, 2, 0, 0, 0, time.FixedZone("UTC+3", 3*3600)), 0, []string{"orders_2026_01", "orders_2026_02", "orders_2026_03"}},
	}
	for _, c := range cases {
		got := names(Expired(parts, c.now, c.keep))
		if !equal(got, c.want) {
			t.Errorf("Expired(%v, %d) = %v, want %v", c.now, c.keep, got, c.want)
		}
	}
}

// TestPostgres — DDL на настоящем PostgreSQL профиля labs, причём в
// сессии с поясом Asia/Tokyo: без смещения в литералах границы
// уехали бы на 9 часов.
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
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`SET TimeZone = 'Asia/Tokyo'`)
	exec(`DROP TABLE IF EXISTS orders`)
	exec(`CREATE TABLE orders (id bigint NOT NULL, created_at timestamptz NOT NULL) PARTITION BY RANGE (created_at)`)
	parts, err := Monthly("orders", utc(2026, 1, 15, 0), utc(2026, 4, 1, 0))
	if err != nil || len(parts) != 3 {
		t.Fatalf("Monthly = %v, %v", names(parts), err)
	}
	for range 2 { // второй проход — IF NOT EXISTS
		for _, p := range parts {
			sql, err := p.CreateSQL()
			if err != nil {
				t.Fatal(err)
			}
			exec(sql)
		}
	}
	moments := []time.Time{
		utc(2026, 1, 1, 0),
		utc(2026, 2, 1, 0).Add(-time.Microsecond),
		utc(2026, 2, 1, 0),
		utc(2026, 3, 31, 23),
	}
	for i, m := range moments {
		want, ok := Locate(parts, m)
		if !ok {
			t.Fatalf("Locate(%v) не нашёл партицию", m)
		}
		exec(`INSERT INTO orders VALUES ($1, $2)`, i, m)
		var got string
		if err := conn.QueryRow(ctx, `SELECT tableoid::regclass::text FROM orders WHERE id = $1`, i).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want.Name {
			t.Errorf("строка %v легла в %s, Locate говорит %s", m, got, want.Name)
		}
	}
	for _, p := range Expired(parts, utc(2026, 3, 10, 0), 1) {
		sql, err := p.DetachSQL()
		if err != nil {
			t.Fatal(err)
		}
		exec(sql)
		exec(`DROP TABLE ` + p.Name)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("после отсоединения января строк %d, want 2", n)
	}
}

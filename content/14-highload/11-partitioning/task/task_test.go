package task

import (
	"errors"
	"testing"
	"time"
)

func utc(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

func names(parts []Partition) []string {
	out := []string{}
	for _, p := range parts {
		out = append(out, p.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMonthly(t *testing.T) {
	parts, err := Monthly("orders", utc(2026, 1, 15, 0), utc(2026, 4, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"orders_2026_01", "orders_2026_02", "orders_2026_03"}
	if got := names(parts); !equal(got, want) {
		t.Fatalf("имена = %v, want %v", got, want)
	}
	p := parts[1]
	if p.Parent != "orders" || !p.From.Equal(utc(2026, 2, 1, 0)) || !p.To.Equal(utc(2026, 3, 1, 0)) {
		t.Fatalf("февраль = %+v", p)
	}
}

func TestCreateSQL(t *testing.T) {
	parts, err := Monthly("orders", utc(2026, 12, 1, 0), utc(2027, 1, 1, 0))
	if err != nil || len(parts) != 1 {
		t.Fatalf("Monthly = %v, %v", parts, err)
	}
	got, err := parts[0].CreateSQL()
	want := "CREATE TABLE IF NOT EXISTS orders_2026_12 PARTITION OF orders FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00')"
	if err != nil || got != want {
		t.Fatalf("CreateSQL = %q, %v\nwant %q", got, err, want)
	}
	got, err = parts[0].DetachSQL()
	if want := "ALTER TABLE orders DETACH PARTITION orders_2026_12"; err != nil || got != want {
		t.Fatalf("DetachSQL = %q, %v; want %q", got, err, want)
	}
}

func TestLocate(t *testing.T) {
	parts, _ := Monthly("orders", utc(2026, 1, 1, 0), utc(2026, 3, 1, 0))
	p, ok := Locate(parts, utc(2026, 2, 1, 0))
	if !ok || p.Name != "orders_2026_02" {
		t.Fatalf("Locate(1 февраля) = %q, %v; want orders_2026_02", p.Name, ok)
	}
	if _, ok := Locate(parts, utc(2026, 3, 1, 0)); ok {
		t.Fatal("1 марта не входит ни в одну партицию")
	}
}

func TestExpired(t *testing.T) {
	parts, _ := Monthly("orders", utc(2026, 1, 1, 0), utc(2026, 6, 1, 0))
	got := names(Expired(parts, utc(2026, 5, 15, 12), 2))
	if want := []string{"orders_2026_01", "orders_2026_02"}; !equal(got, want) {
		t.Fatalf("Expired = %v, want %v", got, want)
	}
}

func TestBadName(t *testing.T) {
	if _, err := Monthly("Orders", utc(2026, 1, 1, 0), utc(2026, 2, 1, 0)); !errors.Is(err, ErrBadName) {
		t.Fatalf("err = %v, want ErrBadName", err)
	}
}

package task

import (
	"testing"
	"time"
)

type Cents int64

func TestSumNamedTypes(t *testing.T) {
	if got := Sum([]Cents{150, 250}); got != 400 {
		t.Errorf("Sum([]Cents) = %d, want 400", got)
	}
	if got := Sum([]time.Duration{time.Second, time.Second}); got != 2*time.Second {
		t.Errorf("Sum([]time.Duration) = %v, want 2s", got)
	}
	if got := Sum[int](nil); got != 0 {
		t.Errorf("Sum(nil) = %d, want 0", got)
	}
}

func TestClampEdges(t *testing.T) {
	if got := Clamp("m", "a", "k"); got != "k" {
		t.Errorf(`Clamp("m", "a", "k") = %q, want "k"`, got)
	}
	if got := Clamp(1.5, 1.5, 1.5); got != 1.5 {
		t.Errorf("Clamp(1.5, 1.5, 1.5) = %v, want 1.5", got)
	}
	if got := Clamp(0, 0, 10); got != 0 {
		t.Errorf("Clamp(0, 0, 10) = %d, want 0", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("Clamp(5, 10, 0) должен паниковать")
		}
	}()
	Clamp(5, 10, 0)
}

type item struct {
	name  string
	price float64
}

func TestMaxByEdges(t *testing.T) {
	got, ok := MaxBy([]item(nil), func(i item) float64 { return i.price })
	if ok || got != (item{}) {
		t.Errorf("MaxBy(nil) = %v, %v; want нулевое значение, false", got, ok)
	}
	items := []item{{"a", 1}, {"b", 3}, {"c", 3}, {"d", -1}}
	got, ok = MaxBy(items, func(i item) float64 { return i.price })
	if !ok || got.name != "b" {
		t.Errorf("MaxBy при равных ключах = %v, want первый — b", got)
	}
	neg := []item{{"x", -5}, {"y", -2}}
	got, _ = MaxBy(neg, func(i item) float64 { return i.price })
	if got.name != "y" {
		t.Errorf("MaxBy с отрицательными ключами = %v, want y", got)
	}
}

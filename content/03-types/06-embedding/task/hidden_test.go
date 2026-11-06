package task

import (
	"errors"
	"testing"
)

func TestReadOnlyOverCounting(t *testing.T) {
	m := MapStore{"k": "v"}
	c := &CountingStore{Store: m}
	var s Store = ReadOnlyStore{c}

	if v, ok := s.Get("k"); !ok || v != "v" {
		t.Errorf(`Get("k") = %q, %v; want "v", true`, v, ok)
	}
	if err := s.Set("k", "new"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Set = %v, want ErrReadOnly", err)
	}
	if c.Gets != 1 {
		t.Errorf("Get через ReadOnlyStore не дошёл до CountingStore: Gets = %d, want 1", c.Gets)
	}
	if c.Sets != 0 {
		t.Errorf("Set прошёл сквозь ReadOnlyStore: Sets = %d, want 0", c.Sets)
	}
	if m["k"] != "v" {
		t.Errorf("значение изменилось через ReadOnlyStore: %q", m["k"])
	}
}

type failingStore struct{ MapStore }

var errDisk = errors.New("disk full")

func (failingStore) Set(key, value string) error { return errDisk }

func TestCountingPassesErrors(t *testing.T) {
	c := &CountingStore{Store: failingStore{MapStore{}}}
	if err := c.Set("a", "1"); !errors.Is(err, errDisk) {
		t.Errorf("Set = %v, want ошибку встроенного хранилища", err)
	}
	if c.Sets != 1 {
		t.Errorf("неудачный Set не посчитан: Sets = %d, want 1", c.Sets)
	}
}

func TestNestedCounting(t *testing.T) {
	inner := &CountingStore{Store: MapStore{}}
	outer := &CountingStore{Store: inner}
	outer.Set("a", "1")
	outer.Get("a")
	outer.Get("a")
	if inner.Gets != 2 || inner.Sets != 1 || outer.Gets != 2 || outer.Sets != 1 {
		t.Errorf("inner %d/%d, outer %d/%d; want 2/1 у обоих", inner.Gets, inner.Sets, outer.Gets, outer.Sets)
	}
}

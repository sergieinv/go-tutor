package task

import (
	"errors"
	"testing"
)

func TestCountingStore(t *testing.T) {
	c := &CountingStore{Store: MapStore{}}
	if err := c.Set("a", "1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, ok := c.Get("a")
	c.Get("missing")
	if !ok || v != "1" {
		t.Errorf(`Get("a") = %q, %v; want "1", true`, v, ok)
	}
	if c.Gets != 2 || c.Sets != 1 {
		t.Errorf("Gets, Sets = %d, %d; want 2, 1", c.Gets, c.Sets)
	}
}

func TestReadOnlyStore(t *testing.T) {
	m := MapStore{"a": "1"}
	var s Store = ReadOnlyStore{m}
	if err := s.Set("a", "2"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Set = %v, want ErrReadOnly", err)
	}
	if v, _ := s.Get("a"); v != "1" {
		t.Errorf(`Get("a") = %q, want "1"`, v)
	}
}

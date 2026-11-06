package task

import (
	"errors"
	"strings"
	"testing"
)

func TestCacheGet(t *testing.T) {
	calls := map[string]int{}
	c := NewCache(func(k string) (string, error) {
		calls[k]++
		return strings.ToUpper(k), nil
	})
	for range 3 {
		for _, k := range []string{"a", "b"} {
			v, err := c.Get(k)
			if err != nil || v != strings.ToUpper(k) {
				t.Fatalf("Get(%q) = (%q, %v), want (%q, nil)", k, v, err, strings.ToUpper(k))
			}
		}
	}
	if calls["a"] != 1 || calls["b"] != 1 {
		t.Errorf("load вызван %v, want по одному разу на ключ", calls)
	}
}

func TestCacheErrorIsCached(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	c := NewCache(func(k int) (int, error) {
		calls++
		return -1, boom
	})
	for range 2 {
		v, err := c.Get(7)
		if !errors.Is(err, boom) || v != -1 {
			t.Fatalf("Get(7) = (%d, %v), want (-1, boom)", v, err)
		}
	}
	if calls != 1 {
		t.Errorf("load вызван %d раз, want 1: ошибка тоже запоминается", calls)
	}
}

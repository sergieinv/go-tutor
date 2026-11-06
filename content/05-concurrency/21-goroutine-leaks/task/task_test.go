package task

import (
	"context"
	"maps"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLookup(t *testing.T) {
	got, err := Lookup(context.Background(), []string{"go", "cs"}, strings.ToUpper)
	want := map[string]string{"go": "GO", "cs": "CS"}
	if err != nil || !maps.Equal(got, want) {
		t.Fatalf("Lookup = (%v, %v), want (%v, nil)", got, err, want)
	}
}

func TestTotal(t *testing.T) {
	got := Total([]int{1, 2, 3, 4}, func(n int) int { return n * n })
	if got != 30 {
		t.Fatalf("Total = %d, want 30", got)
	}
}

func TestEvery(t *testing.T) {
	var calls atomic.Int32
	stop := Every(2*time.Millisecond, func() { calls.Add(1) })
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stop()
	if n := calls.Load(); n < 3 {
		t.Fatalf("за 2 с f вызвана %d раз, want хотя бы 3", n)
	}
}

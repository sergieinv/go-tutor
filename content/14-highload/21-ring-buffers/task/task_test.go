package task

import (
	"runtime"
	"testing"
	"time"
)

func TestCapRoundUp(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{1, 1}, {2, 2}, {3, 4}, {5, 8}, {16, 16}, {17, 32}} {
		if got := New[int](tc.in).Cap(); got != tc.want {
			t.Errorf("New(%d).Cap() = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestFIFOFullEmpty(t *testing.T) {
	r := New[string](2)
	if !r.TryPush("a") || !r.TryPush("b") {
		t.Fatal("TryPush в неполное кольцо вернул false")
	}
	if r.TryPush("c") {
		t.Fatal("TryPush в полное кольцо вернул true")
	}
	if r.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", r.Len())
	}
	for _, want := range []string{"a", "b"} {
		if v, ok := r.TryPop(); !ok || v != want {
			t.Fatalf("TryPop() = %q, %v; want %q, true", v, ok, want)
		}
	}
	if v, ok := r.TryPop(); ok || v != "" {
		t.Fatalf("TryPop() на пустом = %q, %v; want \"\", false", v, ok)
	}
}

func TestSPSCStress(t *testing.T) {
	const n = 10_000
	r := New[int](4)
	if r.Cap() != 4 {
		t.Fatalf("Cap() = %d, want 4", r.Cap())
	}
	go func() {
		for i := 0; i < n; {
			if r.TryPush(i) {
				i++
			} else {
				runtime.Gosched()
			}
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for want := 0; want < n; {
		v, ok := r.TryPop()
		if !ok {
			if time.Now().After(deadline) {
				t.Fatalf("застряли на значении %d", want)
			}
			runtime.Gosched()
			continue
		}
		if v != want {
			t.Fatalf("получили %d, ждали %d", v, want)
		}
		want++
	}
}

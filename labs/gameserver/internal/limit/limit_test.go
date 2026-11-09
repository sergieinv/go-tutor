package limit

import (
	"testing"
	"time"
)

func TestBucket(t *testing.T) {
	t0 := time.Unix(0, 0)
	b := New(10, 3, t0)
	for i := 0; i < 3; i++ {
		if !b.Allow(t0) {
			t.Fatalf("burst token %d denied", i)
		}
	}
	if b.Allow(t0) {
		t.Fatal("must be limited after burst")
	}
	if !b.Allow(t0.Add(100 * time.Millisecond)) { // +1 жетон
		t.Fatal("token must refill")
	}
	if b.Allow(t0.Add(100 * time.Millisecond)) {
		t.Fatal("only one token refilled")
	}
}

package task

import (
	"strconv"
	"testing"
)

func TestParams(t *testing.T) {
	b := NewBloom(1000, 0.01)
	if b.Bits() != 9586 || b.K() != 7 {
		t.Fatalf("NewBloom(1000, 0.01): m = %d, k = %d; want 9586, 7", b.Bits(), b.K())
	}
}

func TestNoFalseNegatives(t *testing.T) {
	b := NewBloom(1000, 0.01)
	for i := range 1000 {
		b.Add("user-" + strconv.Itoa(i))
	}
	for i := range 1000 {
		if k := "user-" + strconv.Itoa(i); !b.MayContain(k) {
			t.Fatalf("MayContain(%q) = false для добавленного ключа", k)
		}
	}
	fp := 0
	for i := range 10_000 {
		if b.MayContain("guest-" + strconv.Itoa(i)) {
			fp++
		}
	}
	if fp > 200 {
		t.Errorf("ложных срабатываний %d из 10000, want не больше 200 (p = 1%%)", fp)
	}
}

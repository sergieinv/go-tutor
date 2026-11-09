package task

import (
	"hash/fnv"
	"testing"
)

// fnvHash — обычная хеш-функция для тестов без коллизий по построению.
func fnvHash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// hv собирает хеш из номера стартовой группы (h1) и h2.
func hv(h1, h2 uint64) uint64 { return h1<<7 | h2 }

// fixed — хеш-функция по таблице: так тесты управляют группой и h2.
func fixed(t *testing.T, m map[string]uint64) func(string) uint64 {
	return func(s string) uint64 {
		h, ok := m[s]
		if !ok {
			t.Fatalf("хеш для ключа %q не задан в тесте", s)
		}
		return h
	}
}

func TestPutGetUpdate(t *testing.T) {
	tb := New(4, fnvHash)
	for i, k := range []string{"go", "c#", "rust", "zig"} {
		if err := tb.Put(k, i); err != nil {
			t.Fatalf("Put(%q) = %v", k, err)
		}
	}
	if err := tb.Put("go", 100); err != nil {
		t.Fatalf("Put(go, 100) = %v", err)
	}
	if v, ok := tb.Get("go"); !ok || v != 100 {
		t.Errorf("Get(go) = %d, %v; want 100, true", v, ok)
	}
	if v, ok := tb.Get("rust"); !ok || v != 2 {
		t.Errorf("Get(rust) = %d, %v; want 2, true", v, ok)
	}
	if _, ok := tb.Get("java"); ok {
		t.Errorf("Get(java) нашёл отсутствующий ключ")
	}
	if tb.Len() != 4 {
		t.Errorf("Len() = %d, want 4 (обновление не добавляет ключ)", tb.Len())
	}
}

func TestCtrlStoresH2(t *testing.T) {
	tb := New(2, fixed(t, map[string]uint64{"a": hv(1, 0x2a), "b": hv(3, 0x15)}))
	tb.Put("a", 1)
	tb.Put("b", 2) // h1 = 3, а групп две: стартовая группа 3 & 1 = 1
	want := [8]byte{0x2a, 0x15, Empty, Empty, Empty, Empty, Empty, Empty}
	if got := tb.Ctrl(1); got != want {
		t.Errorf("Ctrl(1) = %#v, want %#v", got, want)
	}
	if got := tb.Ctrl(0); got != [8]byte{Empty, Empty, Empty, Empty, Empty, Empty, Empty, Empty} {
		t.Errorf("Ctrl(0) = %#v, want все Empty", got)
	}
}

func TestDeleteInSparseGroup(t *testing.T) {
	tb := New(1, fixed(t, map[string]uint64{"a": hv(0, 1), "b": hv(0, 2)}))
	tb.Put("a", 1)
	tb.Put("b", 2)
	if !tb.Delete("a") {
		t.Fatalf("Delete(a) = false, want true")
	}
	if c := tb.Ctrl(0)[0]; c != Empty {
		t.Errorf("после Delete в неполной группе Ctrl(0)[0] = %#x, want Empty (0x80)", c)
	}
	if _, ok := tb.Get("a"); ok {
		t.Errorf("Get(a) после Delete нашёл ключ")
	}
	if v, ok := tb.Get("b"); !ok || v != 2 {
		t.Errorf("Get(b) = %d, %v; want 2, true", v, ok)
	}
	if tb.Len() != 1 {
		t.Errorf("Len() = %d, want 1", tb.Len())
	}
}

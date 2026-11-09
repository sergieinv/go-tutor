package task

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"
)

// collide возвращает n ключей с одной стартовой группой g и разными h2.
func collide(prefix string, g uint64, n int, m map[string]uint64) []string {
	keys := make([]string, n)
	for i := range n {
		k := fmt.Sprintf("%s%d", prefix, i)
		m[k] = hv(g, uint64(0x10+i))
		keys[i] = k
	}
	return keys
}

func mustPut(t *testing.T, tb *Table, k string, v int) {
	t.Helper()
	if err := tb.Put(k, v); err != nil {
		t.Fatalf("Put(%q) = %v", k, err)
	}
}

func allEmpty() [8]byte {
	return [8]byte{Empty, Empty, Empty, Empty, Empty, Empty, Empty, Empty}
}

func TestNewPanicsOnBadGroups(t *testing.T) {
	for _, n := range []int{0, -4, 3, 6} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("New(%d, ...) должен паниковать", n)
				}
			}()
			New(n, fnvHash)
		}()
	}
}

func TestNewAllEmpty(t *testing.T) {
	tb := New(8, fnvHash)
	for g := range 8 {
		if got := tb.Ctrl(g); got != allEmpty() {
			t.Fatalf("Ctrl(%d) новой таблицы = %#v, want все Empty", g, got)
		}
	}
	if tb.Len() != 0 {
		t.Errorf("Len() новой таблицы = %d", tb.Len())
	}
}

func TestSameH2DifferentKeys(t *testing.T) {
	// Полный хеш совпадает: h2 одинаковый, различить ключи можно только сравнением.
	tb := New(2, fixed(t, map[string]uint64{"x": hv(0, 0x33), "y": hv(0, 0x33), "z": hv(0, 0x33)}))
	mustPut(t, tb, "x", 1)
	mustPut(t, tb, "y", 2)
	if v, ok := tb.Get("x"); !ok || v != 1 {
		t.Errorf("Get(x) = %d, %v; want 1, true", v, ok)
	}
	if v, ok := tb.Get("y"); !ok || v != 2 {
		t.Errorf("Get(y) = %d, %v; want 2, true", v, ok)
	}
	if _, ok := tb.Get("z"); ok {
		t.Errorf("Get(z) нашёл ключ только по совпадению h2")
	}
	if tb.Len() != 2 {
		t.Errorf("Len() = %d, want 2", tb.Len())
	}
}

func TestProbeSequence(t *testing.T) {
	// 4 группы, старт — группа 1. Последовательность: 1, 2, 0, 3.
	m := map[string]uint64{}
	keys := collide("k", 1, 32, m)
	tb := New(4, fixed(t, m))
	order := []int{1, 2, 0, 3}
	for i, k := range keys {
		mustPut(t, tb, k, i)
		g := order[i/8]
		if c := tb.Ctrl(g)[i%8]; c != byte(0x10+i) {
			t.Fatalf("ключ №%d должен лечь в группу %d, слот %d: Ctrl(%d) = %#v", i, g, i%8, g, tb.Ctrl(g))
		}
	}
	for i, k := range keys {
		if v, ok := tb.Get(k); !ok || v != i {
			t.Errorf("Get(%q) = %d, %v; want %d, true", k, v, ok, i)
		}
	}
}

func TestTombstoneInFullGroup(t *testing.T) {
	m := map[string]uint64{}
	keys := collide("k", 0, 9, m) // 8 заполнят группу 0, девятый уйдёт в группу 1
	tb := New(2, fixed(t, m))
	for i, k := range keys {
		mustPut(t, tb, k, i)
	}
	if !tb.Delete(keys[3]) {
		t.Fatalf("Delete(%q) = false", keys[3])
	}
	if c := tb.Ctrl(0)[3]; c != Deleted {
		t.Fatalf("удаление из полной группы: Ctrl(0)[3] = %#x, want Deleted (0xFE)", c)
	}
	// С пустым слотом вместо надгробия поиск остановился бы в группе 0.
	if v, ok := tb.Get(keys[8]); !ok || v != 8 {
		t.Errorf("Get(%q) после удаления соседа = %d, %v; want 8, true", keys[8], v, ok)
	}
	if _, ok := tb.Get(keys[3]); ok {
		t.Errorf("Get удалённого ключа нашёл его")
	}
	if tb.Len() != 8 {
		t.Errorf("Len() = %d, want 8", tb.Len())
	}
}

func TestPutReusesTombstone(t *testing.T) {
	m := map[string]uint64{"new": hv(0, 0x55)}
	keys := collide("k", 0, 9, m)
	tb := New(2, fixed(t, m))
	for i, k := range keys {
		mustPut(t, tb, k, i)
	}
	tb.Delete(keys[5])
	mustPut(t, tb, "new", 42)
	if c := tb.Ctrl(0)[5]; c != 0x55 {
		t.Errorf("новый ключ должен занять надгробие: Ctrl(0) = %#v", tb.Ctrl(0))
	}
	if got := tb.Ctrl(1); got[1] != Empty {
		t.Errorf("новый ключ не должен занимать пустой слот группы 1: Ctrl(1) = %#v", got)
	}
	if v, ok := tb.Get("new"); !ok || v != 42 {
		t.Errorf("Get(new) = %d, %v; want 42, true", v, ok)
	}
	if tb.Len() != 9 {
		t.Errorf("Len() = %d, want 9", tb.Len())
	}
}

func TestUpdateBeyondTombstoneNoDuplicate(t *testing.T) {
	m := map[string]uint64{}
	keys := collide("k", 0, 9, m)
	tb := New(2, fixed(t, m))
	for i, k := range keys {
		mustPut(t, tb, k, i)
	}
	tb.Delete(keys[0]) // надгробие в группе 0, ключ keys[8] — в группе 1
	mustPut(t, tb, keys[8], 800)
	if tb.Len() != 8 {
		t.Fatalf("Len() = %d, want 8: обновление существующего ключа не должно вставлять копию в надгробие", tb.Len())
	}
	if c := tb.Ctrl(0)[0]; c != Deleted {
		t.Errorf("Ctrl(0)[0] = %#x: обновление заняло надгробие", c)
	}
	if v, _ := tb.Get(keys[8]); v != 800 {
		t.Errorf("Get(%q) = %d, want 800", keys[8], v)
	}
	if !tb.Delete(keys[8]) {
		t.Fatalf("Delete(%q) = false", keys[8])
	}
	if _, ok := tb.Get(keys[8]); ok {
		t.Errorf("после Delete ключ всё ещё находится — в таблице была копия")
	}
}

func TestFirstEmptySlot(t *testing.T) {
	tb := New(1, fixed(t, map[string]uint64{"a": hv(0, 1), "b": hv(0, 2), "c": hv(0, 3), "d": hv(0, 4)}))
	mustPut(t, tb, "a", 1)
	mustPut(t, tb, "b", 2)
	mustPut(t, tb, "c", 3)
	tb.Delete("b")
	mustPut(t, tb, "d", 4)
	want := [8]byte{1, 4, 3, Empty, Empty, Empty, Empty, Empty}
	if got := tb.Ctrl(0); got != want {
		t.Errorf("Ctrl(0) = %#v, want %#v: вставка идёт в первый пустой слот", got, want)
	}
}

func TestErrFull(t *testing.T) {
	m := map[string]uint64{"extra": hv(0, 0x70)}
	keys := collide("k", 0, 8, m)
	tb := New(1, fixed(t, m))
	for i, k := range keys {
		mustPut(t, tb, k, i)
	}
	if err := tb.Put("extra", 9); !errors.Is(err, ErrFull) {
		t.Fatalf("Put в полную таблицу = %v, want ErrFull", err)
	}
	if _, ok := tb.Get("extra"); ok || tb.Len() != 8 {
		t.Errorf("после ErrFull таблица изменилась: Len = %d", tb.Len())
	}
	if err := tb.Put(keys[2], 200); err != nil {
		t.Errorf("обновление в полной таблице = %v, want nil", err)
	}
	if v, _ := tb.Get(keys[2]); v != 200 {
		t.Errorf("Get(%q) = %d, want 200", keys[2], v)
	}
	// Удаление из полной группы оставляет надгробие — и место для одного ключа.
	tb.Delete(keys[6])
	if c := tb.Ctrl(0)[6]; c != Deleted {
		t.Errorf("Ctrl(0)[6] = %#x, want Deleted", c)
	}
	for i, k := range keys {
		if _, ok := tb.Get(k); ok == (i == 6) {
			t.Errorf("Get(%q) в полной таблице с надгробием: ok = %v", k, ok)
		}
	}
	mustPut(t, tb, "extra", 9)
	if c := tb.Ctrl(0)[6]; c != 0x70 {
		t.Errorf("Ctrl(0)[6] = %#x, want 0x70: вставка в надгробие", c)
	}
	if err := tb.Put(keys[6], 6); !errors.Is(err, ErrFull) {
		t.Errorf("Put снова в полную таблицу = %v, want ErrFull", err)
	}
}

func TestDeleteMissing(t *testing.T) {
	tb := New(2, fnvHash)
	mustPut(t, tb, "a", 1)
	if tb.Delete("b") {
		t.Errorf("Delete отсутствующего ключа = true")
	}
	if tb.Delete("a") != true || tb.Delete("a") != false {
		t.Errorf("повторный Delete должен вернуть false")
	}
	if tb.Len() != 0 {
		t.Errorf("Len() = %d, want 0", tb.Len())
	}
}

// TestRandomAgainstMap сверяет таблицу с map на случайных операциях
// с сильными коллизиями по h1 и h2 (фиксированный seed — тест детерминирован).
func TestRandomAgainstMap(t *testing.T) {
	const groups = 4
	hash := func(s string) uint64 {
		h := fnvHash(s)
		return hv(h&3, (h>>20)&3)
	}
	r := rand.New(rand.NewPCG(1, 2))
	tb := New(groups, hash)
	model := map[string]int{}
	for step := range 20000 {
		k := fmt.Sprintf("key%d", r.IntN(48))
		switch op := r.IntN(3); op {
		case 0:
			v := r.IntN(1000)
			err := tb.Put(k, v)
			_, exists := model[k]
			full := !exists && len(model) == groups*GroupSlots
			switch {
			case full && !errors.Is(err, ErrFull):
				t.Fatalf("шаг %d: Put(%q) в полную таблицу = %v, want ErrFull", step, k, err)
			case !full && err != nil:
				t.Fatalf("шаг %d: Put(%q) = %v, want nil (свободных слотов %d)", step, k, err, groups*GroupSlots-len(model))
			case !full:
				model[k] = v
			}
		case 1:
			_, exists := model[k]
			if got := tb.Delete(k); got != exists {
				t.Fatalf("шаг %d: Delete(%q) = %v, want %v", step, k, got, exists)
			}
			delete(model, k)
		case 2:
			want, exists := model[k]
			if v, ok := tb.Get(k); ok != exists || v != want {
				t.Fatalf("шаг %d: Get(%q) = %d, %v; want %d, %v", step, k, v, ok, want, exists)
			}
		}
		if tb.Len() != len(model) {
			t.Fatalf("шаг %d: Len() = %d, want %d", step, tb.Len(), len(model))
		}
	}
	used := 0
	for g := range groups {
		for _, c := range tb.Ctrl(g) {
			if c != Empty && c != Deleted {
				used++
			}
		}
	}
	if used != len(model) {
		t.Errorf("занятых контрольных байтов %d, а ключей %d", used, len(model))
	}
}

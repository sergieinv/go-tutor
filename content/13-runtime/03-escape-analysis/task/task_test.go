package task

import (
	"errors"
	"slices"
	"testing"
)

func TestAppendKey(t *testing.T) {
	got := string(AppendKey([]byte("key="), "alice", 42))
	if got != "key=alice:42" {
		t.Fatalf(`AppendKey("key=", "alice", 42) = %q, want "key=alice:42"`, got)
	}
}

// Аргументы — в переменных пакета: константы компилятор
// упаковал бы в интерфейс ещё при сборке, без выделения.
var (
	openUser = "alice"
	openID   = 123456
)

func TestAppendKeyNoAllocs(t *testing.T) {
	buf := make([]byte, 0, 64)
	allocs := testing.AllocsPerRun(100, func() {
		buf = AppendKey(buf[:0], openUser, openID)
	})
	if allocs != 0 {
		t.Errorf("AppendKey: %v выделений на вызов, want 0", allocs)
	}
}

func TestParseKV(t *testing.T) {
	k, v, ok := ParseKV(" host = example.org ")
	if k != "host" || v != "example.org" || !ok {
		t.Fatalf(`ParseKV(" host = example.org ") = %q, %q, %v; want "host", "example.org", true`, k, v, ok)
	}
	if k, v, ok := ParseKV("no separator"); k != "" || v != "" || ok {
		t.Errorf(`ParseKV("no separator") = %q, %q, %v; want "", "", false`, k, v, ok)
	}
}

func TestSortByLen(t *testing.T) {
	words := []string{"ccc", "bb", "a", "aa", "dddd"}
	SortByLen(words)
	want := []string{"a", "aa", "bb", "ccc", "dddd"}
	if !slices.Equal(words, want) {
		t.Errorf("SortByLen = %q, want %q", words, want)
	}
}

func TestMean(t *testing.T) {
	if m, err := Mean([]float64{1, 2, 3, 6}); m != 3 || err != nil {
		t.Errorf("Mean([1 2 3 6]) = %v, %v; want 3, nil", m, err)
	}
	if _, err := Mean(nil); !errors.Is(err, ErrEmpty) {
		t.Errorf("Mean(nil): err = %v, want ErrEmpty", err)
	}
}

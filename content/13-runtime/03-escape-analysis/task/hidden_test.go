package task

import (
	"errors"
	"math"
	"slices"
	"testing"
)

// Глобальные переменные, чтобы компилятор не выбросил вызовы
// и не подставил аргументы как константы.
var (
	argUser   = "user-with-long-name"
	argID     = -987654321
	argLine   = "  timeout = 30s  "
	argBad    = "garbage"
	sinkStr   string
	sinkFloat float64
	sinkErr   error
	sinkBool  bool
)

func TestHiddenAppendKeyValues(t *testing.T) {
	tests := []struct {
		dst  string
		user string
		id   int
		want string
	}{
		{"", "bob", 0, "bob:0"},
		{"", "", 7, ":7"},
		{"x/", "eve", -15, "x/eve:-15"},
		{"", "u", math.MaxInt64, "u:9223372036854775807"},
		{"", "u", math.MinInt64, "u:-9223372036854775808"},
		{"", "юзер", 1, "юзер:1"},
	}
	for _, tt := range tests {
		got := string(AppendKey([]byte(tt.dst), tt.user, tt.id))
		if got != tt.want {
			t.Errorf("AppendKey(%q, %q, %d) = %q, want %q", tt.dst, tt.user, tt.id, got, tt.want)
		}
	}
	if got := string(AppendKey(nil, "a", 1)); got != "a:1" {
		t.Errorf(`AppendKey(nil, "a", 1) = %q, want "a:1"`, got)
	}
}

func TestHiddenAppendKeyKeepsDst(t *testing.T) {
	// Как append: пишет в тот же массив, если хватает ёмкости.
	buf := make([]byte, 2, 32)
	copy(buf, "k=")
	out := AppendKey(buf, "id", 5)
	if string(out) != "k=id:5" {
		t.Fatalf("AppendKey = %q, want %q", out, "k=id:5")
	}
	if &out[0] != &buf[0] {
		t.Errorf("AppendKey выделил новый массив, хотя ёмкости dst хватало")
	}
}

func TestHiddenParseKVCases(t *testing.T) {
	tests := []struct {
		line, key, value string
		ok               bool
	}{
		{"a=b", "a", "b", true},
		{"a=b=c", "a", "b=c", true},
		{"=x", "", "x", true},
		{"k=", "k", "", true},
		{"\tname\t=\t Go \n", "name", "Go", true},
		{"=", "", "", true},
		{"", "", "", false},
		{"   ", "", "", false},
		{"key value", "", "", false},
	}
	for _, tt := range tests {
		k, v, ok := ParseKV(tt.line)
		if k != tt.key || v != tt.value || ok != tt.ok {
			t.Errorf("ParseKV(%q) = %q, %q, %v; want %q, %q, %v", tt.line, k, v, ok, tt.key, tt.value, tt.ok)
		}
	}
}

func TestHiddenSortByLenCases(t *testing.T) {
	tests := []struct {
		in, want []string
	}{
		{nil, nil},
		{[]string{}, []string{}},
		{[]string{"b", "a", "c"}, []string{"a", "b", "c"}},
		{[]string{"go", "", "z", "go"}, []string{"", "z", "go", "go"}},
		// Длина — в байтах: «я» занимает 2 байта, как «ab».
		{[]string{"я", "abc", "ab", "x"}, []string{"x", "ab", "я", "abc"}},
		{[]string{"dd", "cc", "bb", "aa", "e"}, []string{"e", "aa", "bb", "cc", "dd"}},
	}
	for _, tt := range tests {
		got := slices.Clone(tt.in)
		SortByLen(got)
		if !slices.Equal(got, tt.want) {
			t.Errorf("SortByLen(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHiddenMeanCases(t *testing.T) {
	if m, err := Mean([]float64{5}); m != 5 || err != nil {
		t.Errorf("Mean([5]) = %v, %v; want 5, nil", m, err)
	}
	if m, err := Mean([]float64{-1, 1}); m != 0 || err != nil {
		t.Errorf("Mean([-1 1]) = %v, %v; want 0, nil", m, err)
	}
	m, err := Mean([]float64{})
	if m != 0 || !errors.Is(err, ErrEmpty) {
		t.Errorf("Mean([]) = %v, %v; want 0, ErrEmpty", m, err)
	}
}

func TestHiddenNoAllocs(t *testing.T) {
	buf := make([]byte, 0, 64)
	words := []string{"pear", "fig", "apple", "kiwi", "banana", "plum", "date", "lime"}
	shuffled := slices.Clone(words)
	xs := []float64{1.5, 2.5, 3.5}
	checks := []struct {
		name string
		f    func()
	}{
		{"AppendKey", func() { buf = AppendKey(buf[:0], argUser, argID) }},
		{"ParseKV", func() { sinkStr, _, sinkBool = ParseKV(argLine) }},
		{"ParseKV без '='", func() { sinkStr, _, sinkBool = ParseKV(argBad) }},
		{"SortByLen", func() {
			copy(shuffled, words)
			SortByLen(shuffled)
		}},
		{"Mean", func() { sinkFloat, sinkErr = Mean(xs) }},
		{"Mean(nil)", func() { sinkFloat, sinkErr = Mean(nil) }},
	}
	for _, c := range checks {
		if allocs := testing.AllocsPerRun(200, c.f); allocs != 0 {
			t.Errorf("%s: %v выделений на вызов, want 0", c.name, allocs)
		}
	}
}

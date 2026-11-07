package task

import (
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// Fuzz-тест без флага -fuzz прогоняет только seed-корпус —
// здесь собраны входы, на которых ломаются наивные реализации.
func FuzzEscapeRoundTrip(f *testing.F) {
	for _, s := range []string{
		"", "a", ",", "\n", `\`, `\\`, `\n`, `\,`, `\\n`, `\\,`, `a\`,
		"\\\n", ",\\", "\xff\xfe", "привет, мир", "\\\\\\", `\n\,\\`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		enc := Escape(s)
		if strings.Contains(enc, "\n") {
			t.Errorf("Escape(%q) = %q: в результате остался перевод строки", s, enc)
		}
		dec, err := Unescape(enc)
		if err != nil || dec != s {
			t.Errorf("Unescape(Escape(%q)) = %q, %v; want %q, nil", s, dec, err, s)
		}
		// Экранированное поле — ровно одно поле: запятые в нём не делят строку.
		if got, err := Split(enc); err != nil || len(got) != 1 || got[0] != s {
			t.Errorf("Split(Escape(%q)) = %q, %v; want [%q], nil", s, got, err, s)
		}
	})
}

func FuzzSplitJoin(f *testing.F) {
	f.Add("a", "b", "c")
	f.Add("", "", "")
	f.Add(`\`, ",", `\,`)
	f.Add("x,y", `z\`, "\n")
	f.Add(`\\`, `\n`, ",,")
	f.Fuzz(func(t *testing.T, a, b, c string) {
		for _, fields := range [][]string{{a}, {a, b}, {a, b, c}} {
			line := Join(fields)
			got, err := Split(line)
			if err != nil || !slices.Equal(got, fields) {
				t.Errorf("Split(Join(%q)) = %q, %v; want %q, nil", fields, got, err, fields)
			}
		}
	})
}

func TestUnescapeErrors(t *testing.T) {
	for _, s := range []string{`\`, `abc\`, `\x`, `a\tb`, `\\\`, `\N`, `\ `, "\\\n"} {
		got, err := Unescape(s)
		if !errors.Is(err, ErrSyntax) {
			t.Errorf("Unescape(%q) = %q, %v; want ErrSyntax", s, got, err)
		} else if got != "" {
			t.Errorf("Unescape(%q) при ошибке вернула %q, want \"\"", s, got)
		}
	}
}

func TestUnescapeRaw(t *testing.T) {
	// Неэкранированные запятые и переводы строк Unescape оставляет как есть.
	for _, tt := range []struct{ in, want string }{
		{"a,b", "a,b"},
		{"a\nb", "a\nb"},
		{`\\n`, `\n`},
		{`\\\n`, "\\\n"},
		{`\\,`, `\,`},
		{"\xff", "\xff"},
	} {
		if got, err := Unescape(tt.in); err != nil || got != tt.want {
			t.Errorf("Unescape(%q) = %q, %v; want %q, nil", tt.in, got, err, tt.want)
		}
	}
}

func TestSplitCases(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{""}},
		{",", []string{"", ""}},
		{"a,,b", []string{"a", "", "b"}},
		{`a\,b`, []string{"a,b"}},
		{`a\\,b`, []string{`a\`, "b"}},
		{`a\\\,b`, []string{`a\,b`}},
		{`\n,\,`, []string{"\n", ","}},
	}
	for _, tt := range tests {
		got, err := Split(tt.in)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("Split(%q) = %q, %v; want %q, nil", tt.in, got, err, tt.want)
		}
	}
}

func TestSplitErrors(t *testing.T) {
	for _, s := range []string{`a\`, `a,b\`, `a\x,b`, `ok,\q`} {
		got, err := Split(s)
		if !errors.Is(err, ErrSyntax) || got != nil {
			t.Errorf("Split(%q) = %q, %v; want nil, ErrSyntax", s, got, err)
		}
	}
}

func TestJoinEmpty(t *testing.T) {
	if got := Join(nil); got != "" {
		t.Errorf("Join(nil) = %q, want \"\"", got)
	}
	if got := Join([]string{"", ""}); got != "," {
		t.Errorf(`Join(["", ""]) = %q, want ","`, got)
	}
}

// Property-тест на псевдослучайных входах: фиксированный seed
// делает прогон воспроизводимым, алфавит — богатым на ловушки.
func TestPropertiesRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(2024, 7))
	alphabet := []string{`\`, ",", "n", "\n", "a", "ж", "\xff", " "}
	gen := func() string {
		var b strings.Builder
		for range r.IntN(12) {
			b.WriteString(alphabet[r.IntN(len(alphabet))])
		}
		return b.String()
	}
	for range 3000 {
		s := gen()
		if dec, err := Unescape(Escape(s)); err != nil || dec != s {
			t.Fatalf("Unescape(Escape(%q)) = %q, %v", s, dec, err)
		}
		fields := make([]string, 1+r.IntN(4))
		for i := range fields {
			fields[i] = gen()
		}
		if got, err := Split(Join(fields)); err != nil || !slices.Equal(got, fields) {
			t.Fatalf("Split(Join(%q)) = %q, %v", fields, got, err)
		}
		// Произвольная строка: Unescape не паникует и при успехе
		// повторное экранирование даёт эквивалентную строку.
		raw := gen()
		if dec, err := Unescape(raw); err == nil {
			if again, err := Unescape(Escape(dec)); err != nil || again != dec {
				t.Fatalf("Unescape(%q) = %q, но повторный цикл даёт %q, %v", raw, dec, again, err)
			}
		} else if !errors.Is(err, ErrSyntax) {
			t.Fatalf("Unescape(%q): ошибка %v, want ErrSyntax", raw, err)
		}
	}
}

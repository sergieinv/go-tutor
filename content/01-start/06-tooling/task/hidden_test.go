package task

import "testing"

func TestCountVerbsEdges(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   int
	}{
		{"empty", "", 0},
		{"only percent literal", "100%%", 0},
		{"double literal", "%%%%", 0},
		{"literal then verb", "%%%d", 1},
		{"three verbs", "%v%v%v", 3},
		{"all flags", "%+d %-d %#x % d %08d", 5},
		{"flags combined", "%-+#08.3f", 1},
		{"precision without digits", "%.f", 1},
		{"width and precision", "%10.4e", 1},
		{"unicode text", "Привет, %s! Ёлка: %d шт.", 2},
		{"lone percent", "%", -1},
		{"percent at end", "abc%", -1},
		{"odd percents", "%%%", -1},
		{"width without verb", "%5", -1},
		{"precision without verb", "%.2", -1},
		{"flag without verb", "%-", -1},
		{"bang", "%!", -1},
		{"newline after percent", "%\n", -1},
		{"star width", "%*d", -1},
		{"explicit index", "%[1]d", -1},
		{"non-ASCII verb", "%ё", -1},
		{"valid then broken", "%d and %", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountVerbs(tt.format); got != tt.want {
				t.Errorf("CountVerbs(%q) = %d, want %d", tt.format, got, tt.want)
			}
		})
	}
}

func TestCheckCallEdges(t *testing.T) {
	tests := []struct {
		name   string
		format string
		nargs  int
		want   string
	}{
		{"no verbs no args", "hello", 0, ""},
		{"extra arg", "%d", 2, "format reads 1 args, but call has 2"},
		{"arg without verbs", "total", 1, "format reads 0 args, but call has 1"},
		{"percent literal", "50%%", 0, ""},
		{"bad format wins over count", "%", 5, "bad format"},
		{"star is bad", "%*d", 2, "bad format"},
		{"many", "%s=%v (%T) %q", 4, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckCall(tt.format, tt.nargs); got != tt.want {
				t.Errorf("CheckCall(%q, %d) = %q, want %q", tt.format, tt.nargs, got, tt.want)
			}
		})
	}
}

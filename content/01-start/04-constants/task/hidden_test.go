package task

import (
	"math"
	"testing"
)

func TestHumanSizeEdges(t *testing.T) {
	tests := []struct {
		name string
		n    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"below KiB", 1023, "1023 B"},
		{"exactly KiB", 1024, "1.0 KiB"},
		{"almost MiB", 1048575, "1024.0 KiB"},
		{"fraction MiB", 2621440, "2.5 MiB"},
		{"exactly TiB", 1 << 40, "1.0 TiB"},
		{"beyond TiB", 5 << 50, "5120.0 TiB"},
		{"near max", 1 << 62, "4194304.0 TiB"},
		{"negative KiB", -1536, "-1.5 KiB"},
		{"negative bytes", -5, "-5 B"},
		{"max int64", math.MaxInt64, "8388608.0 TiB"},
		{"min int64", math.MinInt64, "-8388608.0 TiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HumanSize(tt.n); got != tt.want {
				t.Errorf("HumanSize(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

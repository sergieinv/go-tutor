package task

import (
	"errors"
	"strconv"
	"testing"
)

func TestParseSizeSyntaxEdge(t *testing.T) {
	bad := []string{"+5", "1.5K", "10KB", " 10", "10 ", "K", "G", "1_000", "0x10", "10KK", "١٢", "5T"}
	for _, in := range bad {
		got, err := ParseSize(in)
		if !errors.Is(err, strconv.ErrSyntax) || got != 0 {
			t.Errorf("ParseSize(%q) = %d, %v; want 0 и ErrSyntax", in, got, err)
		}
	}
}

func TestParseSizeRange(t *testing.T) {
	bad := []string{
		"9223372036854775808",   // MaxInt64 + 1
		"99999999999999999999",  // больше uint64
		"8589934592G",           // 2^33 * 2^30 = 2^63
		"9007199254740992K",     // 2^53 * 2^10 = 2^63
		"18446744073709551615M", // MaxUint64
	}
	for _, in := range bad {
		got, err := ParseSize(in)
		if !errors.Is(err, strconv.ErrRange) || got != 0 {
			t.Errorf("ParseSize(%q) = %d, %v; want 0 и ErrRange", in, got, err)
		}
	}
}

func TestParseSizeLimits(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"9223372036854775807", 9223372036854775807},
		{"8589934591G", 8589934591 << 30},
		{"9007199254740991K", 9007199254740991 << 10},
		{"007K", 7168},
		{"0G", 0},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d, nil", tt.in, got, err, tt.want)
		}
	}
}

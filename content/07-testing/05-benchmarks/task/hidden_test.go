package task

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

// reference — заведомо верная (но медленная) реализация.
func reference(nums []int, sep string) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, sep)
}

func TestJoinIntsEdge(t *testing.T) {
	tests := []struct {
		name string
		nums []int
		sep  string
	}{
		{"nil", nil, ","},
		{"пустой", []int{}, ","},
		{"пустой разделитель", []int{1, 23, 456}, ""},
		{"многобайтовый разделитель", []int{1, -2, 3}, " · "},
		{"границы int", []int{math.MinInt, math.MaxInt, 0, -1}, ","},
		{"степени десяти", []int{9, 10, 99, 100, 999, 1000, -9, -10, -100}, " "},
		{"длинный разделитель", []int{1, 2}, strings.Repeat("-", 100)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := reference(tt.nums, tt.sep)
			if got := JoinInts(tt.nums, tt.sep); got != want {
				t.Errorf("JoinInts(%v, %q) = %q, want %q", tt.nums, tt.sep, got, want)
			}
			if got := string(AppendInts(nil, tt.nums, tt.sep)); got != want {
				t.Errorf("AppendInts(nil, %v, %q) = %q, want %q", tt.nums, tt.sep, got, want)
			}
		})
	}
}

func TestJoinIntsRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	seps := []string{",", "", ", ", "→"}
	for range 500 {
		nums := make([]int, r.IntN(30))
		for i := range nums {
			switch r.IntN(3) {
			case 0:
				nums[i] = r.IntN(100) - 50
			case 1:
				nums[i] = int(r.Int64())
			default:
				nums[i] = -int(r.Int64())
			}
		}
		sep := seps[r.IntN(len(seps))]
		want := reference(nums, sep)
		if got := JoinInts(nums, sep); got != want {
			t.Fatalf("JoinInts(%v, %q) = %q, want %q", nums, sep, got, want)
		}
		if got := string(AppendInts([]byte("x"), nums, sep)); got != "x"+want {
			t.Fatalf("AppendInts(\"x\", %v, %q) = %q, want %q", nums, sep, got, "x"+want)
		}
	}
}

func TestJoinIntsAllocsBig(t *testing.T) {
	nums := make([]int, 1000)
	for i := range nums {
		nums[i] = (i - 500) * 1_000_003
	}
	nums[0], nums[1] = math.MinInt, math.MaxInt
	allocs := testing.AllocsPerRun(20, func() {
		JoinInts(nums, " | ")
	})
	if allocs > 1 {
		t.Errorf("JoinInts на 1000 чисел: %.0f аллокаций, want <= 1", allocs)
	}
}

func TestJoinIntsEmptyNoAllocs(t *testing.T) {
	allocs := testing.AllocsPerRun(20, func() {
		JoinInts(nil, ",")
		JoinInts([]int{}, ", ")
	})
	if allocs != 0 {
		t.Errorf("JoinInts на пустом входе: %.0f аллокаций, want 0", allocs)
	}
}

func TestAppendIntsNoAllocs(t *testing.T) {
	nums := []int{math.MinInt, -1, 0, 1, math.MaxInt, 123456789}
	buf := make([]byte, 0, 4096)
	allocs := testing.AllocsPerRun(50, func() {
		buf = AppendInts(buf[:0], nums, ", ")
	})
	if allocs != 0 {
		t.Errorf("AppendInts при достаточной ёмкости: %.0f аллокаций, want 0", allocs)
	}
	if want := reference(nums, ", "); string(buf) != want {
		t.Errorf("AppendInts = %q, want %q", buf, want)
	}
}

func TestAppendIntsKeepsPrefix(t *testing.T) {
	dst := make([]byte, 0, 64)
	dst = append(dst, "sum:"...)
	got := AppendInts(dst, []int{1, 2}, "+")
	if string(got) != "sum:1+2" {
		t.Errorf("AppendInts = %q, want %q", got, "sum:1+2")
	}
	if &got[0] != &dst[0] {
		t.Errorf("при достаточной ёмкости AppendInts должна писать в тот же массив, что и dst")
	}
	if got := AppendInts([]byte("x"), nil, ","); string(got) != "x" {
		t.Errorf("AppendInts(\"x\", nil) = %q, want %q", got, "x")
	}
}

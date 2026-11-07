package task

import "testing"

func TestJoinInts(t *testing.T) {
	tests := []struct {
		nums []int
		sep  string
		want string
	}{
		{[]int{1, 2, 3}, ", ", "1, 2, 3"},
		{[]int{-5, 0, 42}, ";", "-5;0;42"},
		{[]int{7}, ", ", "7"},
	}
	for _, tt := range tests {
		if got := JoinInts(tt.nums, tt.sep); got != tt.want {
			t.Errorf("JoinInts(%v, %q) = %q, want %q", tt.nums, tt.sep, got, tt.want)
		}
	}
}

func TestAppendInts(t *testing.T) {
	got := AppendInts([]byte("ids="), []int{10, 20}, ",")
	if string(got) != "ids=10,20" {
		t.Errorf("AppendInts = %q, want %q", got, "ids=10,20")
	}
}

func TestJoinIntsAllocs(t *testing.T) {
	nums := make([]int, 100)
	for i := range nums {
		nums[i] = i * 37
	}
	allocs := testing.AllocsPerRun(50, func() {
		JoinInts(nums, ",")
	})
	if allocs > 1 {
		t.Errorf("JoinInts: %.0f аллокаций на вызов, want <= 1", allocs)
	}
}

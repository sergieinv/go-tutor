package task

import "testing"

func constant(n int) func() int {
	return func() int { return n }
}

func TestSumAll(t *testing.T) {
	tests := []struct {
		name    string
		sources []func() int
		want    int
	}{
		{"один", []func() int{constant(7)}, 7},
		{"несколько", []func() int{constant(1), constant(2), constant(3)}, 6},
		{"отрицательные", []func() int{constant(-5), constant(2)}, -3},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SumAll(tt.sources); got != tt.want {
				t.Errorf("SumAll = %d, want %d", got, tt.want)
			}
		})
	}
}

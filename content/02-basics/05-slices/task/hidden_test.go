package task

import (
	"slices"
	"testing"
)

func TestChunkShapes(t *testing.T) {
	tests := []struct {
		name string
		s    []int
		n    int
		want [][]int
	}{
		{"exact", []int{1, 2, 3, 4}, 2, [][]int{{1, 2}, {3, 4}}},
		{"n greater than len", []int{1, 2}, 5, [][]int{{1, 2}}},
		{"n is one", []int{7, 8, 9}, 1, [][]int{{7}, {8}, {9}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Chunk(tt.s, tt.n)
			if !slices.EqualFunc(got, tt.want, slices.Equal[[]int]) {
				t.Errorf("Chunk(%v, %d) = %v, want %v", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestChunkEmpty(t *testing.T) {
	if got := Chunk(nil, 3); len(got) != 0 {
		t.Errorf("Chunk(nil, 3) = %v, want пустой результат", got)
	}
}

func TestChunkNoCopy(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	parts := Chunk(s, 2)
	if len(parts) != 3 {
		t.Fatalf("Chunk вернул %d частей, want 3", len(parts))
	}
	if &parts[0][0] != &s[0] || &parts[2][0] != &s[4] {
		t.Errorf("части должны ссылаться на массив исходного среза, а не на копию")
	}
}

func TestChunkAppendKeepsSource(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	parts := Chunk(s, 2)
	for i := range parts {
		_ = append(parts[i], -1)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(s, want) {
		t.Errorf("append к частям изменил исходный срез: %v, want %v", s, want)
	}
}

func TestChunkPanicsOnNonPositive(t *testing.T) {
	for _, n := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Chunk(s, %d) должен паниковать", n)
				}
			}()
			Chunk([]int{1}, n)
		}()
	}
}

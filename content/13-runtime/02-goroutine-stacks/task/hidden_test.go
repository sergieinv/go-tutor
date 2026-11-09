package task

import (
	"bytes"
	"strings"
	"testing"
)

func TestMaxDepthShapes(t *testing.T) {
	// Самая длинная ветка — справа и не первая на пути.
	tree := &Node{
		Left: &Node{Left: &Node{}},
		Right: &Node{
			Left:  &Node{},
			Right: &Node{Left: &Node{Right: &Node{}}},
		},
	}
	if got := MaxDepth(tree); got != 5 {
		t.Errorf("MaxDepth = %d, want 5", got)
	}
	if got := MaxDepth(&Node{}); got != 1 {
		t.Errorf("MaxDepth(лист) = %d, want 1", got)
	}
}

func TestFloodNoop(t *testing.T) {
	g := grid("ab", "cd")
	for _, tc := range []struct{ r, c int }{{-1, 0}, {0, -1}, {2, 0}, {0, 2}, {1, 5}} {
		if n := Flood(g, tc.r, tc.c, 'x'); n != 0 {
			t.Errorf("Flood(%d, %d) вне сетки = %d, want 0", tc.r, tc.c, n)
		}
	}
	if n := Flood(g, 0, 0, 'a'); n != 0 {
		t.Errorf("Flood в тот же цвет = %d, want 0", n)
	}
	if got := strings.Join(rows(g), ","); got != "ab,cd" {
		t.Errorf("сетка изменилась: %s", got)
	}
	if n := Flood(nil, 0, 0, 'x'); n != 0 {
		t.Errorf("Flood(nil) = %d, want 0", n)
	}
}

func TestFloodDiagonalAndRagged(t *testing.T) {
	g := grid(
		".#.",
		"#.",
		".....",
	)
	// Диагональ не связывает: (1,1) отделена от (0,0).
	if n := Flood(g, 0, 0, 'o'); n != 1 {
		t.Fatalf("Flood(0,0) = %d, want 1: соседи только по сторонам", n)
	}
	// Строки разной длины: (1,1) связана с третьей строкой через (2,1).
	n := Flood(g, 1, 1, 'o')
	want := "o#.,#o,ooooo"
	if got := strings.Join(rows(g), ","); n != 6 || got != want {
		t.Fatalf("Flood(1,1) = %d, сетка %s; want 6, %s", n, got, want)
	}
}

func TestFloodSnakeDeep(t *testing.T) {
	if !deep(t) {
		return
	}
	// Змейка 600×600: коридор шириной в клетку, путь ~180000 клеток.
	const size = 600
	g := make([][]byte, size)
	want := 0
	for i := range g {
		g[i] = bytes.Repeat([]byte{'#'}, size)
		switch {
		case i%2 == 0:
			for j := range g[i] {
				g[i][j] = '.'
			}
			want += size
		case i%4 == 1:
			g[i][size-1] = '.'
			want++
		default:
			g[i][0] = '.'
			want++
		}
	}
	if n := Flood(g, 0, 0, 'o'); n != want {
		t.Fatalf("Flood змейки = %d, want %d", n, want)
	}
	for i, r := range g {
		if bytes.IndexByte(r, '.') >= 0 {
			t.Fatalf("строка %d осталась неперекрашенной: %q…", i, r[:20])
		}
	}
}

func TestMaxDepthDeepRight(t *testing.T) {
	if !deep(t) {
		return
	}
	// Длинная цепочка справа, у каждого узла ещё и короткий левый лист.
	var root *Node
	for range 300_000 {
		root = &Node{Left: &Node{}, Right: root}
	}
	// Самый глубокий — левый лист у последнего узла цепочки.
	if got := MaxDepth(root); got != 300_001 {
		t.Fatalf("MaxDepth = %d, want 300001", got)
	}
}

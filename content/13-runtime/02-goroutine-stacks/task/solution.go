package task

// Node — узел дерева; дерево может быть сколь угодно глубоким.
type Node struct {
	Left, Right *Node
}

// MaxDepth возвращает число узлов на самом длинном пути от корня до листа.
// Обход — с явным стеком в куче: глубина дерева не ограничена стеком горутины.
func MaxDepth(root *Node) int {
	if root == nil {
		return 0
	}
	type item struct {
		n     *Node
		depth int
	}
	stack := []item{{root, 1}}
	best := 0
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		best = max(best, it.depth)
		if it.n.Left != nil {
			stack = append(stack, item{it.n.Left, it.depth + 1})
		}
		if it.n.Right != nil {
			stack = append(stack, item{it.n.Right, it.depth + 1})
		}
	}
	return best
}

// Flood перекрашивает в to область клеток, связанных по сторонам с (r, c)
// и имеющих тот же цвет, что и grid[r][c]. Возвращает число перекрашенных клеток.
func Flood(grid [][]byte, r, c int, to byte) int {
	if r < 0 || r >= len(grid) || c < 0 || c >= len(grid[r]) {
		return 0
	}
	from := grid[r][c]
	if from == to {
		return 0
	}
	type cell struct{ r, c int }
	// Клетку красим, когда кладём в стек, — так она не попадёт туда дважды.
	grid[r][c] = to
	stack := []cell{{r, c}}
	n := 0
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n++
		for _, d := range [4]cell{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nr, nc := cur.r+d.r, cur.c+d.c
			if nr < 0 || nr >= len(grid) || nc < 0 || nc >= len(grid[nr]) || grid[nr][nc] != from {
				continue
			}
			grid[nr][nc] = to
			stack = append(stack, cell{nr, nc})
		}
	}
	return n
}

package task

// Node — узел дерева; дерево может быть сколь угодно глубоким.
type Node struct {
	Left, Right *Node
}

// MaxDepth возвращает число узлов на самом длинном пути от корня до листа.
//
// Работает, но рекурсия: глубина дерева ограничена стеком горутины.
// TODO: перепишите с явным стеком.
func MaxDepth(root *Node) int {
	if root == nil {
		return 0
	}
	return 1 + max(MaxDepth(root.Left), MaxDepth(root.Right))
}

// Flood перекрашивает в to область клеток, связанных по сторонам с (r, c)
// и имеющих тот же цвет, что и grid[r][c]. Возвращает число перекрашенных клеток.
//
// Работает, но рекурсия: на большой сплошной области глубина равна площади.
// TODO: перепишите с явным стеком.
func Flood(grid [][]byte, r, c int, to byte) int {
	if r < 0 || r >= len(grid) || c < 0 || c >= len(grid[r]) {
		return 0
	}
	from := grid[r][c]
	if from == to {
		return 0
	}
	return fill(grid, r, c, from, to)
}

func fill(grid [][]byte, r, c int, from, to byte) int {
	if r < 0 || r >= len(grid) || c < 0 || c >= len(grid[r]) || grid[r][c] != from {
		return 0
	}
	grid[r][c] = to
	return 1 + fill(grid, r-1, c, from, to) + fill(grid, r+1, c, from, to) +
		fill(grid, r, c-1, from, to) + fill(grid, r, c+1, from, to)
}

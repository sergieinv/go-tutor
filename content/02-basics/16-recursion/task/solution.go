package task

// Node — узел двоичного дерева.
type Node struct {
	Val         int
	Left, Right *Node
}

// Sum возвращает сумму всех значений дерева; для nil — 0.
func Sum(n *Node) int {
	if n == nil {
		return 0
	}
	return n.Val + Sum(n.Left) + Sum(n.Right)
}

// Depth возвращает число уровней дерева: nil — 0, один узел — 1.
func Depth(n *Node) int {
	if n == nil {
		return 0
	}
	return 1 + max(Depth(n.Left), Depth(n.Right))
}

// InOrder возвращает значения в порядке «левое, узел, правое».
// Для nil-дерева — пустой срез (не nil).
func InOrder(n *Node) []int {
	out := []int{}
	// Рекурсивное замыкание: переменную объявляем
	// до присваивания, чтобы литерал мог её вызвать.
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		walk(n.Left)
		out = append(out, n.Val)
		walk(n.Right)
	}
	walk(n)
	return out
}

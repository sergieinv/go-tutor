package task

// Node — узел двоичного дерева.
type Node struct {
	Val         int
	Left, Right *Node
}

// Sum возвращает сумму всех значений дерева; для nil — 0.
func Sum(n *Node) int {
	// TODO: реализуйте функцию
	return 0
}

// Depth возвращает число уровней дерева: nil — 0, один узел — 1.
func Depth(n *Node) int {
	// TODO: реализуйте функцию
	return 0
}

// InOrder возвращает значения в порядке «левое, узел, правое».
// Для nil-дерева — пустой срез (не nil).
func InOrder(n *Node) []int {
	// TODO: реализуйте функцию
	return nil
}

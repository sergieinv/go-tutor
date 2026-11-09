package task

// Grid — пространственный хеш: ячейка → id сущностей в ней.
type Grid struct {
	// TODO: размер ячейки, ячейки и сущности
}

// NewGrid создаёт пустую сетку с квадратными ячейками cellSize×cellSize.
func NewGrid(cellSize float64) *Grid {
	return &Grid{}
}

// Insert добавляет сущность; существующий id перемещается.
func (g *Grid) Insert(id int, x, z float64) {
	// TODO
}

// Move перемещает сущность; false — такой нет.
func (g *Grid) Move(id int, x, z float64) bool {
	// TODO
	return false
}

// Remove удаляет сущность; false — такой нет.
func (g *Grid) Remove(id int) bool {
	// TODO
	return false
}

// Len — число сущностей.
func (g *Grid) Len() int {
	// TODO
	return 0
}

// QueryRadius дописывает к dst id сущностей не дальше r от (x, z) по возрастанию.
func (g *Grid) QueryRadius(x, z, r float64, dst []int) []int {
	// TODO
	return dst
}

// QueryAABB дописывает к dst id сущностей в прямоугольнике (границы включены) по возрастанию.
func (g *Grid) QueryAABB(minX, minZ, maxX, maxZ float64, dst []int) []int {
	// TODO
	return dst
}

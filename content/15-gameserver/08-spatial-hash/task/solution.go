package task

import (
	"math"
	"slices"
)

type cellKey struct{ X, Z int }

type entry struct {
	x, z float64
	key  cellKey
	slot int // место id в списке ячейки
}

// Grid — пространственный хеш: ячейка → id сущностей в ней.
type Grid struct {
	size  float64
	cells map[cellKey][]int
	ents  map[int]entry
}

// NewGrid создаёт пустую сетку с квадратными ячейками cellSize×cellSize.
func NewGrid(cellSize float64) *Grid {
	return &Grid{size: cellSize, cells: map[cellKey][]int{}, ents: map[int]entry{}}
}

func (g *Grid) keyOf(x, z float64) cellKey {
	return cellKey{int(math.Floor(x / g.size)), int(math.Floor(z / g.size))}
}

func (g *Grid) link(id int, x, z float64) {
	k := g.keyOf(x, z)
	g.ents[id] = entry{x: x, z: z, key: k, slot: len(g.cells[k])}
	g.cells[k] = append(g.cells[k], id)
}

func (g *Grid) unlink(e entry) {
	list := g.cells[e.key]
	last := list[len(list)-1]
	list[e.slot] = last
	le := g.ents[last]
	le.slot = e.slot
	g.ents[last] = le
	list = list[:len(list)-1]
	if len(list) == 0 {
		delete(g.cells, e.key)
	} else {
		g.cells[e.key] = list
	}
}

// Insert добавляет сущность; существующий id перемещается.
func (g *Grid) Insert(id int, x, z float64) {
	if g.Move(id, x, z) {
		return
	}
	g.link(id, x, z)
}

// Move перемещает сущность; false — такой нет.
func (g *Grid) Move(id int, x, z float64) bool {
	e, ok := g.ents[id]
	if !ok {
		return false
	}
	if k := g.keyOf(x, z); k == e.key {
		e.x, e.z = x, z
		g.ents[id] = e
		return true
	}
	g.unlink(e)
	g.link(id, x, z)
	return true
}

// Remove удаляет сущность; false — такой нет.
func (g *Grid) Remove(id int) bool {
	e, ok := g.ents[id]
	if !ok {
		return false
	}
	g.unlink(e)
	delete(g.ents, id)
	return true
}

// Len — число сущностей.
func (g *Grid) Len() int { return len(g.ents) }

// scan дописывает к dst id из ячеек, пересекающих прямоугольник, для которых keep — true.
func (g *Grid) scan(minX, minZ, maxX, maxZ float64, dst []int, keep func(entry) bool) []int {
	lo, hi := g.keyOf(minX, minZ), g.keyOf(maxX, maxZ)
	filter := func(list []int) {
		for _, id := range list {
			if keep(g.ents[id]) {
				dst = append(dst, id)
			}
		}
	}
	if (float64(hi.X-lo.X)+1)*(float64(hi.Z-lo.Z)+1) > float64(len(g.cells)) {
		for k, list := range g.cells {
			if k.X >= lo.X && k.X <= hi.X && k.Z >= lo.Z && k.Z <= hi.Z {
				filter(list)
			}
		}
		return dst
	}
	for cx := lo.X; cx <= hi.X; cx++ {
		for cz := lo.Z; cz <= hi.Z; cz++ {
			filter(g.cells[cellKey{cx, cz}])
		}
	}
	return dst
}

// QueryRadius дописывает к dst id сущностей не дальше r от (x, z) по возрастанию.
func (g *Grid) QueryRadius(x, z, r float64, dst []int) []int {
	if r < 0 {
		return dst
	}
	start := len(dst)
	dst = g.scan(x-r, z-r, x+r, z+r, dst, func(e entry) bool {
		dx, dz := e.x-x, e.z-z
		return dx*dx+dz*dz <= r*r
	})
	slices.Sort(dst[start:])
	return dst
}

// QueryAABB дописывает к dst id сущностей в прямоугольнике (границы включены) по возрастанию.
func (g *Grid) QueryAABB(minX, minZ, maxX, maxZ float64, dst []int) []int {
	if minX > maxX || minZ > maxZ {
		return dst
	}
	start := len(dst)
	dst = g.scan(minX, minZ, maxX, maxZ, dst, func(e entry) bool {
		return e.x >= minX && e.x <= maxX && e.z >= minZ && e.z <= maxZ
	})
	slices.Sort(dst[start:])
	return dst
}

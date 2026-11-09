// Пространственный хеш: плоскость режется на квадратные ячейки, и для
// каждой непустой ячейки хранится список сущностей в ней. Запрос «кто
// в радиусе R» смотрит только ячейки, которые пересекает квадрат
// [x−R, x+R] × [z−R, z+R], а не все сущности мира. Тесты рядом
// сверяют его с полным перебором и меряют обоих бенчмарком.
package main

import (
	"math"
	"slices"
)

// Ключ ячейки — пара целых: сравнимая структура годится в ключ map
// без своего хеша. Номер ячейки — `floor`, как у чанков: отрицательные
// координаты не должны слипаться с нулевой ячейкой.
type cellKey struct{ X, Z int }

// Сущности — плотный срез по id, без указателей: GC не сканирует
// его содержимое (урок 14-highload/17). `slot` — место сущности в списке
// её ячейки: удаление за O(1), без поиска.
type entity struct {
	x, z  float64
	key   cellKey
	slot  int
	alive bool
}

type Grid struct {
	size  float64
	cells map[cellKey][]int32
	ents  []entity
	// Сколько сущностей проверено запросами: показывает, что
	// перебора всех нет.
	Checked int
}

func NewGrid(cellSize float64) *Grid {
	return &Grid{size: cellSize, cells: map[cellKey][]int32{}}
}

func (g *Grid) keyOf(x, z float64) cellKey {
	return cellKey{int(math.Floor(x / g.size)), int(math.Floor(z / g.size))}
}

// Insert добавляет сущность (id — небольшое неотрицательное число,
// как индекс в срезе сущностей мира).
func (g *Grid) Insert(id int32, x, z float64) {
	if int(id) >= len(g.ents) {
		g.ents = append(g.ents, make([]entity, int(id)+1-len(g.ents))...)
	}
	k := g.keyOf(x, z)
	g.ents[id] = entity{x: x, z: z, key: k, slot: len(g.cells[k]), alive: true}
	g.cells[k] = append(g.cells[k], id)
}

// Удаление из ячейки — «поменять с последним и укоротить»: порядок
// в ячейке не важен, а сдвигать хвост незачем.
func (g *Grid) unlink(id int32) {
	e := &g.ents[id]
	list := g.cells[e.key]
	last := list[len(list)-1]
	list[e.slot] = last
	g.ents[last].slot = e.slot
	list = list[:len(list)-1]
	if len(list) == 0 {
		delete(g.cells, e.key) // пустые ячейки не копим
	} else {
		g.cells[e.key] = list
	}
}

func (g *Grid) Remove(id int32) {
	g.unlink(id)
	g.ents[id].alive = false
}

// Move — самая частая операция: каждая сущность двигается каждый
// тик. Пока ячейка та же, меняются только координаты.
func (g *Grid) Move(id int32, x, z float64) {
	e := &g.ents[id]
	if k := g.keyOf(x, z); k != e.key {
		g.unlink(id)
		e.key, e.slot = k, len(g.cells[k])
		g.cells[k] = append(g.cells[k], id)
	}
	e.x, e.z = x, z
}

// QueryRadius дописывает в dst id сущностей не дальше r от (x, z)
// по возрастанию. Сравниваем квадраты расстояний — без `math.Sqrt`.
// Переданный буфер позволяет запросу не аллоцировать в тике.
func (g *Grid) QueryRadius(x, z, r float64, dst []int32) []int32 {
	start := len(dst)
	lo, hi := g.keyOf(x-r, z-r), g.keyOf(x+r, z+r)
	// Радиус больше мира: ячеек в квадрате запроса больше, чем
	// непустых, — дешевле обойти непустые. Считаем во float64:
	// произведение двух int может переполниться.
	if (float64(hi.X-lo.X)+1)*(float64(hi.Z-lo.Z)+1) > float64(len(g.cells)) {
		for k, list := range g.cells {
			if k.X >= lo.X && k.X <= hi.X && k.Z >= lo.Z && k.Z <= hi.Z {
				dst = g.filter(list, x, z, r, dst)
			}
		}
	} else {
		for cx := lo.X; cx <= hi.X; cx++ {
			for cz := lo.Z; cz <= hi.Z; cz++ {
				dst = g.filter(g.cells[cellKey{cx, cz}], x, z, r, dst)
			}
		}
	}
	slices.Sort(dst[start:])
	return dst
}

func (g *Grid) filter(list []int32, x, z, r float64, dst []int32) []int32 {
	for _, id := range list {
		g.Checked++
		e := &g.ents[id]
		if dx, dz := e.x-x, e.z-z; dx*dx+dz*dz <= r*r {
			dst = append(dst, id)
		}
	}
	return dst
}

// Полный перебор — эталон для тестов и база для бенчмарка.
func (g *Grid) bruteRadius(x, z, r float64, dst []int32) []int32 {
	for id := range g.ents {
		e := &g.ents[id]
		if dx, dz := e.x-x, e.z-z; e.alive && dx*dx+dz*dz <= r*r {
			dst = append(dst, int32(id))
		}
	}
	return dst
}

func main() {}

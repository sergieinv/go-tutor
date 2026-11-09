package world

// grid — пространственный хеш: ячейка = чанк, в ячейке — игроки, которые
// в нём стоят. «Кто рядом?» превращается из перебора всех игроков (O(N))
// в обход (2R+1)² ячеек вокруг — при 200 игроках на большой карте это
// десятки проверок вместо 200.
type grid struct {
	cells map[ChunkPos][]*Player
}

func newGrid() *grid { return &grid{cells: make(map[ChunkPos][]*Player)} }

func (g *grid) add(p *Player, c ChunkPos) {
	g.cells[c] = append(g.cells[c], p)
}

func (g *grid) remove(p *Player, c ChunkPos) {
	cell := g.cells[c]
	for i, q := range cell {
		if q == p {
			last := len(cell) - 1
			cell[i] = cell[last]
			cell[last] = nil
			cell = cell[:last]
			break
		}
	}
	if len(cell) == 0 {
		delete(g.cells, c)
	} else {
		g.cells[c] = cell
	}
}

func (g *grid) move(p *Player, from, to ChunkPos) {
	if from == to {
		return
	}
	g.remove(p, from)
	g.add(p, to)
}

// near вызывает f для каждого игрока в квадрате радиуса r чанков вокруг c.
func (g *grid) near(c ChunkPos, r int32, f func(*Player)) {
	for x := c.X - r; x <= c.X+r; x++ {
		for z := c.Z - r; z <= c.Z+r; z++ {
			for _, p := range g.cells[ChunkPos{x, z}] {
				f(p)
			}
		}
	}
}

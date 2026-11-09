package task

import (
	"cmp"
	"math"
	"slices"
)

// ChunkPos — координаты чанка 16×16 блоков.
type ChunkPos struct{ X, Z int }

// PlayerID — идентификатор игрока.
type PlayerID int

// MaxViewDistance — наибольшая дальность видимости в чанках.
const MaxViewDistance = 32

// ChunkOf возвращает чанк, в котором лежит точка (x, z).
func ChunkOf(x, z float64) ChunkPos {
	return ChunkPos{int(math.Floor(x / 16)), int(math.Floor(z / 16))}
}

// BlockChunk возвращает чанк блока с целыми координатами (bx, bz).
func BlockChunk(bx, bz int) ChunkPos {
	return ChunkPos{bx >> 4, bz >> 4}
}

type player struct {
	center ChunkPos
	r      int
}

// Interest хранит зоны видимости игроков и подписчиков каждого чанка.
type Interest struct {
	players map[PlayerID]*player
	subs    map[ChunkPos]map[PlayerID]struct{}
}

// NewInterest возвращает пустой Interest.
func NewInterest() *Interest {
	return &Interest{
		players: map[PlayerID]*player{},
		subs:    map[ChunkPos]map[PlayerID]struct{}{},
	}
}

func clampR(r int) int { return min(max(r, 0), MaxViewDistance) }

func inView(center, c ChunkPos, r int) bool {
	return abs(c.X-center.X) <= r && abs(c.Z-center.Z) <= r
}

func abs(v int) int { return max(v, -v) }

func dist2(a, b ChunkPos) int {
	dx, dz := a.X-b.X, a.Z-b.Z
	return dx*dx + dz*dz
}

// squareDiff — чанки квадрата (to, toR), которых нет в квадрате (from, fromR);
// fromR < 0 — пустой квадрат.
func squareDiff(from ChunkPos, fromR int, to ChunkPos, toR int) []ChunkPos {
	var out []ChunkPos
	for x := to.X - toR; x <= to.X+toR; x++ {
		for z := to.Z - toR; z <= to.Z+toR; z++ {
			c := ChunkPos{x, z}
			if fromR < 0 || !inView(from, c, fromR) {
				out = append(out, c)
			}
		}
	}
	return out
}

func sortLoad(cs []ChunkPos, center ChunkPos) {
	slices.SortFunc(cs, func(a, b ChunkPos) int {
		return cmp.Or(cmp.Compare(dist2(a, center), dist2(b, center)),
			cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z))
	})
}

func sortUnload(cs []ChunkPos) {
	slices.SortFunc(cs, func(a, b ChunkPos) int {
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z))
	})
}

func (in *Interest) subscribe(id PlayerID, cs []ChunkPos) {
	for _, c := range cs {
		s := in.subs[c]
		if s == nil {
			s = map[PlayerID]struct{}{}
			in.subs[c] = s
		}
		s[id] = struct{}{}
	}
}

func (in *Interest) unsubscribe(id PlayerID, cs []ChunkPos) {
	for _, c := range cs {
		s := in.subs[c]
		delete(s, id)
		if len(s) == 0 {
			delete(in.subs, c)
		}
	}
}

// Join добавляет игрока и возвращает чанки, которые ему нужно загрузить.
func (in *Interest) Join(id PlayerID, x, z float64, r int) []ChunkPos {
	if _, ok := in.players[id]; ok {
		return nil
	}
	p := &player{center: ChunkOf(x, z), r: clampR(r)}
	in.players[id] = p
	load := squareDiff(p.center, -1, p.center, p.r)
	sortLoad(load, p.center)
	in.subscribe(id, load)
	return load
}

func (in *Interest) change(id PlayerID, p *player, c ChunkPos, r int) (load, unload []ChunkPos) {
	if c == p.center && r == p.r {
		return nil, nil
	}
	load = squareDiff(p.center, p.r, c, r)
	unload = squareDiff(c, r, p.center, p.r)
	sortLoad(load, c)
	sortUnload(unload)
	in.unsubscribe(id, unload)
	in.subscribe(id, load)
	p.center, p.r = c, r
	return load, unload
}

// Move перемещает игрока и возвращает разность зон видимости.
func (in *Interest) Move(id PlayerID, x, z float64) (load, unload []ChunkPos) {
	p, ok := in.players[id]
	if !ok {
		return nil, nil
	}
	return in.change(id, p, ChunkOf(x, z), p.r)
}

// SetViewDistance меняет дальность видимости игрока.
func (in *Interest) SetViewDistance(id PlayerID, r int) (load, unload []ChunkPos) {
	p, ok := in.players[id]
	if !ok {
		return nil, nil
	}
	return in.change(id, p, p.center, clampR(r))
}

// Leave удаляет игрока и возвращает чанки его зоны.
func (in *Interest) Leave(id PlayerID) []ChunkPos {
	p, ok := in.players[id]
	if !ok {
		return nil
	}
	unload := squareDiff(p.center, -1, p.center, p.r)
	sortUnload(unload)
	in.unsubscribe(id, unload)
	delete(in.players, id)
	return unload
}

// Subscribers возвращает игроков, в зоне которых лежит чанк, по возрастанию id.
func (in *Interest) Subscribers(c ChunkPos) []PlayerID {
	s := in.subs[c]
	if len(s) == 0 {
		return nil
	}
	out := make([]PlayerID, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Viewers возвращает игроков, которые видят игрока id (кроме него самого).
func (in *Interest) Viewers(id PlayerID) []PlayerID {
	p, ok := in.players[id]
	if !ok {
		return nil
	}
	out := in.Subscribers(p.center)
	out = slices.DeleteFunc(out, func(v PlayerID) bool { return v == id })
	if len(out) == 0 {
		return nil
	}
	return out
}

// BlockChanged возвращает получателей события «блок (bx, bz) изменён».
func (in *Interest) BlockChanged(bx, bz int) []PlayerID {
	return in.Subscribers(BlockChunk(bx, bz))
}

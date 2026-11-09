package task

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestHiddenChunkOfNegativeAndBorders(t *testing.T) {
	cases := []struct {
		x, z float64
		want ChunkPos
	}{
		{-0.0001, 0, ChunkPos{-1, 0}},
		{-16, -16, ChunkPos{-1, -1}},
		{-16.0001, -15.9999, ChunkPos{-2, -1}},
		{-17, 31.999, ChunkPos{-2, 1}},
		{32, -32, ChunkPos{2, -2}},
		{-1000.5, 1000.5, ChunkPos{-63, 62}},
		{29999999.5, -29999999.5, ChunkPos{1874999, -1875000}},
	}
	for _, c := range cases {
		if got := ChunkOf(c.x, c.z); got != c.want {
			t.Errorf("ChunkOf(%v, %v) = %v, want %v", c.x, c.z, got, c.want)
		}
	}
}

func TestHiddenBlockChunk(t *testing.T) {
	cases := []struct {
		bx, bz int
		want   ChunkPos
	}{
		{0, 0, ChunkPos{0, 0}},
		{15, 16, ChunkPos{0, 1}},
		{-1, -16, ChunkPos{-1, -1}},
		{-17, -15, ChunkPos{-2, -1}},
		{-32, 31, ChunkPos{-2, 1}},
	}
	for _, c := range cases {
		if got := BlockChunk(c.bx, c.bz); got != c.want {
			t.Errorf("BlockChunk(%d, %d) = %v, want %v", c.bx, c.bz, got, c.want)
		}
	}
}

func TestHiddenMoveNegativeBorder(t *testing.T) {
	in := NewInterest()
	in.Join(1, 0.5, 0.5, 0)
	load, unload := in.Move(1, -0.5, 0.5)
	if !eqChunks(load, []ChunkPos{{-1, 0}}) || !eqChunks(unload, []ChunkPos{{0, 0}}) {
		t.Fatalf("переход x 0.5 → -0.5: load=%v unload=%v; want [(-1,0)], [(0,0)]", load, unload)
	}
	if got := in.BlockChanged(-1, 5); !eqIDs(got, []PlayerID{1}) {
		t.Fatalf("BlockChanged(-1, 5) = %v, want [1]", got)
	}
	if got := in.BlockChanged(0, 5); len(got) != 0 {
		t.Fatalf("BlockChanged(0, 5) = %v, want пусто: чанк (0,0) выгружен", got)
	}
}

func TestHiddenDiagonalMove(t *testing.T) {
	in := NewInterest()
	in.Join(1, 8, 8, 1)
	load, unload := in.Move(1, 24, 24) // (0,0) → (1,1)
	wantLoad := []ChunkPos{{1, 2}, {2, 1}, {0, 2}, {2, 0}, {2, 2}}
	wantUnload := []ChunkPos{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {1, -1}}
	if !eqChunks(load, wantLoad) || !eqChunks(unload, wantUnload) {
		t.Fatalf("диагональ: load=%v unload=%v; want %v, %v", load, unload, wantLoad, wantUnload)
	}
}

func TestHiddenTeleportFar(t *testing.T) {
	in := NewInterest()
	in.Join(1, 8, 8, 2)
	in.Join(2, 20, 8, 2)
	if got := in.Viewers(2); !eqIDs(got, []PlayerID{1}) {
		t.Fatalf("до телепорта Viewers(2) = %v, want [1]", got)
	}
	load, unload := in.Move(1, 10000, -10000)
	if len(load) != 25 || len(unload) != 25 {
		t.Fatalf("телепорт: загрузить %d, выгрузить %d; want 25 и 25", len(load), len(unload))
	}
	if load[0] != ChunkOf(10000, -10000) {
		t.Fatalf("первым грузится свой чанк %v, got %v", ChunkOf(10000, -10000), load[0])
	}
	for _, c := range unload {
		if got := in.Subscribers(c); slices.Contains(got, 1) {
			t.Fatalf("после телепорта игрок 1 всё ещё подписан на %v", c)
		}
	}
	if got := in.Viewers(2); len(got) != 0 {
		t.Fatalf("после телепорта Viewers(2) = %v, want пусто", got)
	}
	if got := in.Subscribers(ChunkOf(10000, -10000)); !eqIDs(got, []PlayerID{1}) {
		t.Fatalf("Subscribers нового чанка = %v, want [1]", got)
	}
}

func TestHiddenViewDistanceChange(t *testing.T) {
	in := NewInterest()
	in.Join(1, -8, -8, 1) // чанк (-1,-1)
	load, unload := in.SetViewDistance(1, 2)
	if len(unload) != 0 || len(load) != 16 {
		t.Fatalf("r 1 → 2: load %d, unload %d; want 16 и 0", len(load), len(unload))
	}
	for _, c := range load {
		if max(absh(c.X+1), absh(c.Z+1)) != 2 {
			t.Fatalf("r 1 → 2: %v не на кольце радиуса 2", c)
		}
	}
	load, unload = in.SetViewDistance(1, 0)
	if len(load) != 0 || len(unload) != 24 {
		t.Fatalf("r 2 → 0: load %d, unload %d; want 0 и 24", len(load), len(unload))
	}
	if got := in.Subscribers(ChunkPos{-1, -1}); !eqIDs(got, []PlayerID{1}) {
		t.Fatalf("при r = 0 остаётся свой чанк, Subscribers = %v", got)
	}
	load, unload = in.SetViewDistance(1, 0)
	if len(load) != 0 || len(unload) != 0 {
		t.Fatalf("та же дальность: load=%v unload=%v, want пусто", load, unload)
	}
}

func TestHiddenViewDistanceClamp(t *testing.T) {
	in := NewInterest()
	if got := in.Join(1, 0, 0, -5); len(got) != 1 {
		t.Fatalf("Join с r = -5: %d чанков, want 1 (r считается 0)", len(got))
	}
	load, _ := in.SetViewDistance(1, 1000)
	if len(load) != 65*65-1 {
		t.Fatalf("SetViewDistance(1000): %d чанков, want %d (r = 32)", len(load), 65*65-1)
	}
	load, unload := in.SetViewDistance(1, 33)
	if len(load) != 0 || len(unload) != 0 {
		t.Fatalf("33 и 1000 — оба 32: load=%d unload=%d", len(load), len(unload))
	}
}

func TestHiddenUnknownAndDuplicate(t *testing.T) {
	in := NewInterest()
	if l, u := in.Move(7, 1, 1); len(l)+len(u) != 0 {
		t.Fatalf("Move неизвестного: %v %v", l, u)
	}
	if l, u := in.SetViewDistance(7, 3); len(l)+len(u) != 0 {
		t.Fatalf("SetViewDistance неизвестного: %v %v", l, u)
	}
	if got := in.Leave(7); len(got) != 0 {
		t.Fatalf("Leave неизвестного: %v", got)
	}
	if got := in.Viewers(7); len(got) != 0 {
		t.Fatalf("Viewers неизвестного: %v", got)
	}
	in.Join(1, 8, 8, 1)
	if got := in.Join(1, 500, 500, 3); len(got) != 0 {
		t.Fatalf("повторный Join: %v, want пусто", got)
	}
	if got := in.Subscribers(ChunkPos{0, 0}); !eqIDs(got, []PlayerID{1}) {
		t.Fatalf("повторный Join не должен ничего менять: Subscribers(0,0) = %v", got)
	}
	if got := in.Subscribers(ChunkOf(500, 500)); len(got) != 0 {
		t.Fatalf("повторный Join не должен ничего менять: Subscribers = %v", got)
	}
}

func TestHiddenLeave(t *testing.T) {
	in := NewInterest()
	in.Join(1, 8, 8, 1)
	in.Join(2, 8, 8, 1)
	got := in.Leave(1)
	want := []ChunkPos{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 0}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
	if !eqChunks(got, want) {
		t.Fatalf("Leave = %v, want %v", got, want)
	}
	if got := in.Subscribers(ChunkPos{0, 0}); !eqIDs(got, []PlayerID{2}) {
		t.Fatalf("после Leave Subscribers = %v, want [2]", got)
	}
	if got := in.Viewers(2); len(got) != 0 {
		t.Fatalf("после Leave Viewers(2) = %v, want пусто", got)
	}
	// Вышедший может войти снова.
	if got := in.Join(1, 8, 8, 0); !eqChunks(got, []ChunkPos{{0, 0}}) {
		t.Fatalf("Join после Leave = %v", got)
	}
}

// Модель «в лоб»: квадраты игроков и проверка каждого чанка.
type hModel struct {
	center map[PlayerID]ChunkPos
	r      map[PlayerID]int
}

func (m *hModel) square(id PlayerID) map[ChunkPos]bool {
	out := map[ChunkPos]bool{}
	c, r := m.center[id], m.r[id]
	for x := c.X - r; x <= c.X+r; x++ {
		for z := c.Z - r; z <= c.Z+r; z++ {
			out[ChunkPos{x, z}] = true
		}
	}
	return out
}

func hMinus(a, b map[ChunkPos]bool) []ChunkPos {
	var out []ChunkPos
	for c := range a {
		if !b[c] {
			out = append(out, c)
		}
	}
	return out
}

func hLoadOrder(cs []ChunkPos, center ChunkPos) []ChunkPos {
	slices.SortFunc(cs, func(a, b ChunkPos) int {
		return cmp.Or(cmp.Compare(dist2h(a, center), dist2h(b, center)),
			cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z))
	})
	return cs
}

func hUnloadOrder(cs []ChunkPos) []ChunkPos {
	slices.SortFunc(cs, func(a, b ChunkPos) int {
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z))
	})
	return cs
}

func dist2h(a, b ChunkPos) int { return (a.X-b.X)*(a.X-b.X) + (a.Z-b.Z)*(a.Z-b.Z) }

func absh(v int) int { return max(v, -v) }

func TestHiddenRandomAgainstModel(t *testing.T) {
	rng := rand.New(rand.NewPCG(15, 7))
	in := NewInterest()
	m := &hModel{center: map[PlayerID]ChunkPos{}, r: map[PlayerID]int{}}
	coord := func() float64 { return (rng.Float64() - 0.5) * 160 } // чанки -5..4
	for step := range 3000 {
		id := PlayerID(rng.IntN(8))
		_, known := m.center[id]
		switch op := rng.IntN(10); {
		case !known:
			x, z, r := coord(), coord(), rng.IntN(4)
			got := in.Join(id, x, z, r)
			m.center[id], m.r[id] = ChunkOf(x, z), r
			want := hLoadOrder(hMinus(m.square(id), nil), m.center[id])
			if !eqChunks(got, want) {
				t.Fatalf("шаг %d: Join(%d) = %v, want %v", step, id, got, want)
			}
		case op < 6:
			c := m.center[id]
			// Чаще — шаг к соседнему чанку, иногда — телепорт.
			nx, nz := float64(c.X*16)+(rng.Float64()-0.3)*40, float64(c.Z*16)+(rng.Float64()-0.3)*40
			if op == 0 {
				nx, nz = coord(), coord()
			}
			old := m.square(id)
			m.center[id] = ChunkOf(nx, nz)
			now := m.square(id)
			load, unload := in.Move(id, nx, nz)
			if !eqChunks(load, hLoadOrder(hMinus(now, old), m.center[id])) || !eqChunks(unload, hUnloadOrder(hMinus(old, now))) {
				t.Fatalf("шаг %d: Move(%d, %v, %v) = %v, %v", step, id, nx, nz, load, unload)
			}
		case op < 8:
			r := rng.IntN(4)
			old := m.square(id)
			m.r[id] = r
			now := m.square(id)
			load, unload := in.SetViewDistance(id, r)
			if !eqChunks(load, hLoadOrder(hMinus(now, old), m.center[id])) || !eqChunks(unload, hUnloadOrder(hMinus(old, now))) {
				t.Fatalf("шаг %d: SetViewDistance(%d, %d) = %v, %v", step, id, r, load, unload)
			}
		default:
			want := hUnloadOrder(hMinus(m.square(id), nil))
			delete(m.center, id)
			delete(m.r, id)
			if got := in.Leave(id); !eqChunks(got, want) {
				t.Fatalf("шаг %d: Leave(%d) = %v, want %v", step, id, got, want)
			}
		}
		if step%50 != 0 {
			continue
		}
		// Подписчики каждого чанка и видимость игроков — как у модели.
		for x := -9; x <= 8; x++ {
			for z := -9; z <= 8; z++ {
				c := ChunkPos{x, z}
				var want []PlayerID
				for id := range m.center {
					if absh(c.X-m.center[id].X) <= m.r[id] && absh(c.Z-m.center[id].Z) <= m.r[id] {
						want = append(want, id)
					}
				}
				slices.Sort(want)
				if got := in.Subscribers(c); !eqIDs(got, want) {
					t.Fatalf("шаг %d: Subscribers(%v) = %v, want %v", step, c, got, want)
				}
				if got := in.BlockChanged(x*16+rng.IntN(16), z*16+rng.IntN(16)); !eqIDs(got, want) {
					t.Fatalf("шаг %d: BlockChanged в чанке %v = %v, want %v", step, c, got, want)
				}
			}
		}
		for id := range m.center {
			var want []PlayerID
			for v := range m.center {
				c := m.center[id]
				if v != id && absh(c.X-m.center[v].X) <= m.r[v] && absh(c.Z-m.center[v].Z) <= m.r[v] {
					want = append(want, v)
				}
			}
			slices.Sort(want)
			if got := in.Viewers(id); !eqIDs(got, want) {
				t.Fatalf("шаг %d: Viewers(%d) = %v, want %v", step, id, got, want)
			}
		}
	}
}

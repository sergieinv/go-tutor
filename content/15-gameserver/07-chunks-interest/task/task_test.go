package task

import (
	"fmt"
	"slices"
	"testing"
)

func eqChunks(a, b []ChunkPos) bool { return len(a) == 0 && len(b) == 0 || slices.Equal(a, b) }
func eqIDs(a, b []PlayerID) bool    { return len(a) == 0 && len(b) == 0 || slices.Equal(a, b) }

func TestChunkOf(t *testing.T) {
	cases := []struct {
		x, z float64
		want ChunkPos
	}{
		{0, 0, ChunkPos{0, 0}},
		{15.99, 8, ChunkPos{0, 0}},
		{16, 8, ChunkPos{1, 0}},
		{40, 100, ChunkPos{2, 6}},
		{-0.5, -0.5, ChunkPos{-1, -1}},
	}
	for _, c := range cases {
		if got := ChunkOf(c.x, c.z); got != c.want {
			t.Errorf("ChunkOf(%v, %v) = %v, want %v", c.x, c.z, got, c.want)
		}
	}
}

func TestJoinLoadsSquareNearestFirst(t *testing.T) {
	in := NewInterest()
	got := in.Join(1, 8, 8, 1)
	want := []ChunkPos{
		{0, 0},
		{-1, 0}, {0, -1}, {0, 1}, {1, 0},
		{-1, -1}, {-1, 1}, {1, -1}, {1, 1},
	}
	if !eqChunks(got, want) {
		t.Fatalf("Join = %v, want %v", got, want)
	}
}

func TestMoveWithinChunkSendsNothing(t *testing.T) {
	in := NewInterest()
	in.Join(1, 1, 1, 2)
	load, unload := in.Move(1, 15.5, 15.5)
	if len(load) != 0 || len(unload) != 0 {
		t.Fatalf("Move внутри чанка: load=%v unload=%v, want пусто", load, unload)
	}
}

func TestMoveAcrossBorder(t *testing.T) {
	in := NewInterest()
	in.Join(1, 8, 8, 1)
	load, unload := in.Move(1, 16.5, 8)
	wantLoad := []ChunkPos{{2, 0}, {2, -1}, {2, 1}}
	wantUnload := []ChunkPos{{-1, -1}, {-1, 0}, {-1, 1}}
	if !eqChunks(load, wantLoad) || !eqChunks(unload, wantUnload) {
		t.Fatalf("Move = %v, %v; want %v, %v", load, unload, wantLoad, wantUnload)
	}
}

func TestBlockChangedGoesToSubscribersOnly(t *testing.T) {
	in := NewInterest()
	in.Join(1, 8, 8, 1)   // чанк (0,0), видит x,z ∈ [-1, 1]
	in.Join(2, 40, 8, 1)  // чанк (2,0), видит x ∈ [1, 3]
	in.Join(3, 200, 8, 1) // чанк (12,0), далеко
	cases := []struct {
		bx, bz int
		want   []PlayerID
	}{
		{20, 3, []PlayerID{1, 2}}, // чанк (1,0)
		{3, 3, []PlayerID{1}},     // чанк (0,0)
		{50, 3, []PlayerID{2}},    // чанк (3,0)
		{100, 3, nil},             // чанк (6,0): никто
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.bx, ",", c.bz), func(t *testing.T) {
			if got := in.BlockChanged(c.bx, c.bz); !eqIDs(got, c.want) {
				t.Fatalf("BlockChanged = %v, want %v", got, c.want)
			}
		})
	}
}

func TestViewers(t *testing.T) {
	in := NewInterest()
	in.Join(1, 8, 8, 1)
	in.Join(2, 24, 8, 1)
	in.Join(3, 200, 8, 1)
	if got := in.Viewers(1); !eqIDs(got, []PlayerID{2}) {
		t.Fatalf("Viewers(1) = %v, want [2]", got)
	}
	if got := in.Viewers(3); len(got) != 0 {
		t.Fatalf("Viewers(3) = %v, want пусто", got)
	}
}

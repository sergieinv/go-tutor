package task

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
)

// spawns — независимая модель появления: те же правила, что в
// условии, для мира без занятых клеток, кроме busy.
func spawns(seed uint64, size int32, n int, busy map[Pos]bool) []Pos {
	r := rand.NewPCG(seed, 0)
	var out []Pos
	for len(out) < n {
		p := Pos{int32(r.Uint64() % uint64(size)), int32(r.Uint64() % uint64(size))}
		if !busy[p] {
			busy[p] = true
			out = append(out, p)
		}
	}
	return out
}

func mustSubmit(t *testing.T, s *Sim, ins ...Input) {
	t.Helper()
	for _, in := range ins {
		if err := s.Submit(in); err != nil {
			t.Fatalf("Submit(%+v) = %v", in, err)
		}
	}
}

func TestJoinSpawnsFromSeed(t *testing.T) {
	s := NewSim(7, 100)
	mustSubmit(t, s, Input{Tick: 1, Player: 2, Kind: Join}, Input{Tick: 1, Player: 1, Kind: Join})
	s.Step()
	want := spawns(7, 100, 2, map[Pos]bool{})
	// Порядок применения — по Player: сначала 1, потом 2.
	got := s.Players()
	if len(got) != 2 || got[0] != (PlayerState{1, want[0]}) || got[1] != (PlayerState{2, want[1]}) {
		t.Fatalf("Players = %+v, want [{1 %v} {2 %v}]", got, want[0], want[1])
	}
	if s.Tick() != 1 {
		t.Errorf("Tick = %d, want 1", s.Tick())
	}
}

func TestMoveAndCollision(t *testing.T) {
	// Мир 3×3. Ищем сид, при котором игроки 1 и 2 появились в одной
	// строке: в x = 0 и x = 2, а клетка между ними свободна.
	var seed uint64
	for ; ; seed++ {
		p := spawns(seed, 3, 2, map[Pos]bool{})
		if p[0].X == 0 && p[1].X == 2 && p[0].Z == p[1].Z {
			break
		}
	}
	s := NewSim(seed, 3)
	mustSubmit(t, s, Input{Tick: 1, Player: 1, Kind: Join}, Input{Tick: 1, Player: 2, Kind: Join})
	s.Step()
	ps := s.Players()
	if len(ps) != 2 {
		t.Fatalf("после входа игроков %d, want 2", len(ps))
	}
	z := ps[0].Pos.Z
	// Оба хотят в (1, z): первым применяется игрок 1.
	mustSubmit(t, s, Input{Tick: 2, Player: 2, Kind: Move, DX: -1}, Input{Tick: 2, Player: 1, Kind: Move, DX: 1})
	s.Step()
	want := []PlayerState{{1, Pos{1, z}}, {2, Pos{2, z}}}
	if got := s.Players(); !slices.Equal(got, want) {
		t.Fatalf("Players = %+v, want %+v", got, want)
	}
}

func TestSameInputsSameHash(t *testing.T) {
	run := func(reverse bool) uint64 {
		s := NewSim(42, 16)
		var ins []Input
		for p := range uint32(10) {
			ins = append(ins, Input{Tick: 1, Player: p + 1, Kind: Join})
			for tick := uint64(2); tick <= 20; tick++ {
				ins = append(ins, Input{Tick: tick, Player: p + 1, Kind: Move, DX: int32(tick%3) - 1, DZ: int32(p%3) - 1})
			}
		}
		if reverse {
			slices.Reverse(ins)
		}
		mustSubmit(t, s, ins...)
		for range 20 {
			s.Step()
		}
		return s.Hash()
	}
	a, b := run(false), run(true)
	if a == 0 || a != b {
		t.Fatalf("Hash = %#x и %#x: при тех же вводах хеш должен совпасть и не быть нулём", a, b)
	}
}

func TestLateAndBadInput(t *testing.T) {
	s := NewSim(1, 8)
	s.Step()
	if err := s.Submit(Input{Tick: 1, Player: 1, Kind: Join}); !errors.Is(err, ErrLate) {
		t.Errorf("ввод на прошедший тик: %v, want ErrLate", err)
	}
	if err := s.Submit(Input{Tick: 5, Player: 1, Kind: 9}); !errors.Is(err, ErrBadInput) {
		t.Errorf("неизвестный Kind: %v, want ErrBadInput", err)
	}
	if got := len(s.Log()); got != 0 {
		t.Errorf("в журнале %d вводов, want 0 — отвергнутые не записываются", got)
	}
}

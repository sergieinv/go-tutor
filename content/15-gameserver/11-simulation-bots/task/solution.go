package task

import (
	"cmp"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"math/rand/v2"
	"slices"
)

type Kind uint8

const (
	Join Kind = iota + 1
	Move
	Leave
)

type Input struct {
	Tick   uint64
	Player uint32
	Kind   Kind
	DX, DZ int32
}

type Pos struct{ X, Z int32 }

type PlayerState struct {
	ID  uint32
	Pos Pos
}

var (
	ErrLate     = errors.New("input for a past tick")
	ErrBadInput = errors.New("unknown input kind")
)

type Sim struct {
	size    int32
	rng     *rand.PCG
	tick    uint64
	players map[uint32]Pos
	cells   map[Pos]uint32
	pending map[uint64][]Input // по тику
	log     []Input
}

func NewSim(seed uint64, size int32) *Sim {
	return &Sim{
		size:    size,
		rng:     rand.NewPCG(seed, 0),
		players: map[uint32]Pos{},
		cells:   map[Pos]uint32{},
		pending: map[uint64][]Input{},
	}
}

func (s *Sim) Tick() uint64 { return s.tick }

func compareInput(a, b Input) int {
	return cmp.Or(
		cmp.Compare(a.Tick, b.Tick),
		cmp.Compare(a.Player, b.Player),
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.DX, b.DX),
		cmp.Compare(a.DZ, b.DZ),
	)
}

func (s *Sim) Submit(in Input) error {
	if in.Kind < Join || in.Kind > Leave {
		return ErrBadInput
	}
	if in.Tick <= s.tick {
		return ErrLate
	}
	s.pending[in.Tick] = append(s.pending[in.Tick], in)
	s.log = append(s.log, in)
	return nil
}

func (s *Sim) Step() {
	s.tick++
	ins := s.pending[s.tick]
	delete(s.pending, s.tick)
	// Канонический порядок, а не порядок прихода.
	slices.SortFunc(ins, compareInput)
	for _, in := range ins {
		s.apply(in)
	}
}

func (s *Sim) apply(in Input) {
	p, ok := s.players[in.Player]
	switch in.Kind {
	case Join:
		if ok || len(s.players) == int(s.size)*int(s.size) {
			return
		}
		for {
			x := int32(s.rng.Uint64() % uint64(s.size))
			z := int32(s.rng.Uint64() % uint64(s.size))
			if _, busy := s.cells[Pos{x, z}]; !busy {
				s.players[in.Player] = Pos{x, z}
				s.cells[Pos{x, z}] = in.Player
				return
			}
		}
	case Move:
		if !ok || in.DX < -1 || in.DX > 1 || in.DZ < -1 || in.DZ > 1 {
			return
		}
		to := Pos{p.X + in.DX, p.Z + in.DZ}
		if to.X < 0 || to.X >= s.size || to.Z < 0 || to.Z >= s.size {
			return
		}
		if id, busy := s.cells[to]; busy && id != in.Player {
			return
		}
		delete(s.cells, p)
		s.cells[to] = in.Player
		s.players[in.Player] = to
	case Leave:
		if ok {
			delete(s.players, in.Player)
			delete(s.cells, p)
		}
	}
}

func (s *Sim) Players() []PlayerState {
	out := make([]PlayerState, 0, len(s.players))
	for id, p := range s.players {
		out = append(out, PlayerState{id, p})
	}
	slices.SortFunc(out, func(a, b PlayerState) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func (s *Sim) Hash() uint64 {
	buf := binary.BigEndian.AppendUint64(nil, s.tick)
	for _, p := range s.Players() {
		buf = binary.BigEndian.AppendUint32(buf, p.ID)
		buf = binary.BigEndian.AppendUint32(buf, uint32(p.Pos.X))
		buf = binary.BigEndian.AppendUint32(buf, uint32(p.Pos.Z))
	}
	h := fnv.New64a()
	h.Write(buf)
	return h.Sum64()
}

func (s *Sim) Log() []Input {
	out := slices.Clone(s.log)
	slices.SortFunc(out, compareInput)
	return out
}

func Replay(seed uint64, size int32, log []Input, ticks int) *Sim {
	s := NewSim(seed, size)
	for _, in := range log {
		s.Submit(in)
	}
	for range ticks {
		s.Step()
	}
	return s
}

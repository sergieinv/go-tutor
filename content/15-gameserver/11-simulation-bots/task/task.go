package task

import "errors"

type Kind uint8

const (
	Join Kind = iota + 1
	Move
	Leave
)

// Input — команда игрока, назначенная на тик Tick.
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
	// TODO
}

func NewSim(seed uint64, size int32) *Sim {
	// TODO
	return &Sim{}
}

// Tick возвращает число выполненных тиков.
func (s *Sim) Tick() uint64 {
	// TODO
	return 0
}

func (s *Sim) Submit(in Input) error {
	// TODO
	return nil
}

func (s *Sim) Step() {
	// TODO
}

func (s *Sim) Players() []PlayerState {
	// TODO
	return nil
}

func (s *Sim) Hash() uint64 {
	// TODO
	return 0
}

func (s *Sim) Log() []Input {
	// TODO
	return nil
}

func Replay(seed uint64, size int32, log []Input, ticks int) *Sim {
	// TODO
	return NewSim(seed, size)
}

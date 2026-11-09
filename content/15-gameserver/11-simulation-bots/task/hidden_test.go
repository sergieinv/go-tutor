package task

import (
	"encoding/binary"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"testing"
)

// session — случайная, но повторимая сессия: входы, ходы
// (в том числе недопустимые), выходы и повторные входы.
func session(seed uint64, players uint32, ticks uint64) []Input {
	r := rand.New(rand.NewPCG(seed, 99))
	var ins []Input
	for tick := uint64(1); tick <= ticks; tick++ {
		for p := uint32(1); p <= players; p++ {
			switch v := r.IntN(20); {
			case v == 0:
				ins = append(ins, Input{Tick: tick, Player: p, Kind: Join})
			case v == 1:
				ins = append(ins, Input{Tick: tick, Player: p, Kind: Leave})
			case v < 18:
				ins = append(ins, Input{Tick: tick, Player: p, Kind: Move, DX: int32(r.IntN(3)) - 1, DZ: int32(r.IntN(3)) - 1})
			case v == 18:
				ins = append(ins, Input{Tick: tick, Player: p, Kind: Move, DX: 2}) // недопустимый шаг
			}
		}
	}
	return ins
}

func hashOf(tick uint64, ps []PlayerState) uint64 {
	buf := binary.BigEndian.AppendUint64(nil, tick)
	for _, p := range ps {
		buf = binary.BigEndian.AppendUint32(buf, p.ID)
		buf = binary.BigEndian.AppendUint32(buf, uint32(p.Pos.X))
		buf = binary.BigEndian.AppendUint32(buf, uint32(p.Pos.Z))
	}
	h := fnv.New64a()
	h.Write(buf)
	return h.Sum64()
}

// Порядок прихода внутри тика не влияет на результат — при любом
// перемешивании (и при любом порядке обхода map внутри решения).
func TestHiddenArrivalOrderDoesNotMatter(t *testing.T) {
	ins := session(1, 30, 40)
	ref := NewSim(5, 8)
	mustSubmit(t, ref, ins...)
	for range 40 {
		ref.Step()
	}
	want := ref.Hash()
	for i := range 20 {
		shuffled := slices.Clone(ins)
		rand.New(rand.NewPCG(uint64(i), 0)).Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})
		s := NewSim(5, 8)
		mustSubmit(t, s, shuffled...)
		for range 40 {
			s.Step()
		}
		if got := s.Hash(); got != want {
			t.Fatalf("перемешивание %d: Hash = %#x, want %#x", i, got, want)
		}
		if !slices.Equal(s.Players(), ref.Players()) {
			t.Fatalf("перемешивание %d: Players отличаются", i)
		}
	}
}

func TestHiddenReplayFromLog(t *testing.T) {
	s := NewSim(11, 6)
	ins := session(2, 12, 30)
	// Ввод приходит по тикам, как в сервере; часть — с опозданием.
	for tick := uint64(1); tick <= 30; tick++ {
		for _, in := range ins {
			if in.Tick == tick {
				mustSubmit(t, s, in)
			}
		}
		if s.Submit(Input{Tick: tick - 1, Player: 1, Kind: Leave}) == nil {
			t.Fatal("ввод на прошедший тик принят")
		}
		s.Step()
	}
	log := s.Log()
	if len(log) != len(ins) {
		t.Fatalf("в журнале %d вводов, want %d — опоздавшие не записываются", len(log), len(ins))
	}
	if !slices.IsSortedFunc(log, func(a, b Input) int {
		if a.Tick != b.Tick {
			return int(a.Tick) - int(b.Tick)
		}
		return int(a.Player) - int(b.Player)
	}) {
		t.Fatal("Log не отсортирован по (Tick, Player)")
	}
	r := Replay(11, 6, log, 30)
	if r.Tick() != 30 || r.Hash() != s.Hash() || !slices.Equal(r.Players(), s.Players()) {
		t.Fatalf("Replay: тик %d хеш %#x, want тик 30 хеш %#x", r.Tick(), r.Hash(), s.Hash())
	}
	// Журнал — копия: изменение результата Log не трогает симуляцию.
	log[0].Player = 999
	if s.Log()[0].Player == 999 {
		t.Error("Log вернул внутренний срез")
	}
}

func TestHiddenHashFormula(t *testing.T) {
	s := NewSim(3, 50)
	if got, want := s.Hash(), hashOf(0, nil); got != want {
		t.Fatalf("пустой мир: Hash = %#x, want %#x", got, want)
	}
	mustSubmit(t, s, session(3, 20, 15)...)
	for range 15 {
		s.Step()
	}
	ps := s.Players()
	if !slices.IsSortedFunc(ps, func(a, b PlayerState) int { return int(a.ID) - int(b.ID) }) {
		t.Fatal("Players не отсортированы по ID")
	}
	if got, want := s.Hash(), hashOf(15, ps); got != want {
		t.Fatalf("Hash = %#x, want %#x (FNV-1a по тику и игрокам в порядке ID)", got, want)
	}
	if hashOf(15, ps) == hashOf(16, ps) {
		t.Fatal("хеш не зависит от тика")
	}
}

// Мир 2×2 вмещает четверых. Пятый вход игнорируется и не тратит
// случайные числа: следующий вход продолжает тот же поток.
func TestHiddenFullWorld(t *testing.T) {
	// model: где появится игрок 6, если после заполнения мира
	// потрачено ещё skip чисел. Выбираем сид, где «лишние» числа
	// заметны.
	model := func(seed uint64, skip int) (fill []Pos, six Pos) {
		r := rand.NewPCG(seed, 0)
		busy := map[Pos]bool{}
		for len(fill) < 4 {
			p := Pos{int32(r.Uint64() % 2), int32(r.Uint64() % 2)}
			if !busy[p] {
				busy[p] = true
				fill = append(fill, p)
			}
		}
		for range skip {
			r.Uint64()
		}
		for {
			p := Pos{int32(r.Uint64() % 2), int32(r.Uint64() % 2)}
			if p != fill[3] { // свободно всё, кроме клетки игрока 4
				return fill, p
			}
		}
	}
	var seed uint64
	for ; ; seed++ {
		_, a := model(seed, 0)
		_, b := model(seed, 2)
		if a != b {
			break
		}
	}
	fill, six := model(seed, 0)
	s := NewSim(seed, 2)
	for p := uint32(1); p <= 5; p++ {
		mustSubmit(t, s, Input{Tick: 1, Player: p, Kind: Join})
	}
	s.Step()
	ps := s.Players()
	if len(ps) != 4 || ps[3].ID != 4 {
		t.Fatalf("Players = %+v, want игроков 1–4", ps)
	}
	for i, p := range ps {
		if p.Pos != fill[i] {
			t.Fatalf("игрок %d в %v, want %v", p.ID, p.Pos, fill[i])
		}
	}
	mustSubmit(t, s,
		Input{Tick: 2, Player: 1, Kind: Leave},
		Input{Tick: 2, Player: 2, Kind: Leave},
		Input{Tick: 2, Player: 3, Kind: Leave},
		Input{Tick: 2, Player: 6, Kind: Join},
	)
	s.Step()
	want := []PlayerState{{4, fill[3]}, {6, six}}
	if got := s.Players(); !slices.Equal(got, want) {
		t.Fatalf("Players = %+v, want %+v — вход в полный мир не должен тратить случайные числа", got, want)
	}
}

func TestHiddenOrderWithinTick(t *testing.T) {
	// Найдём сид, где 1 и 2 появились в соседних по x клетках.
	var seed uint64
	var a Pos
	for ; ; seed++ {
		p := spawns(seed, 4, 2, map[Pos]bool{})
		if p[1].X == p[0].X+1 && p[0].Z == p[1].Z {
			a = p[0]
			break
		}
	}
	join := func() *Sim {
		s := NewSim(seed, 4)
		mustSubmit(t, s, Input{Tick: 1, Player: 1, Kind: Join}, Input{Tick: 1, Player: 2, Kind: Join})
		s.Step()
		return s
	}
	// Игрок 1 уходит, игрок 2 идёт на его место: 1 применяется первым — успех.
	s := join()
	mustSubmit(t, s, Input{Tick: 2, Player: 2, Kind: Move, DX: -1}, Input{Tick: 2, Player: 1, Kind: Leave})
	s.Step()
	if got := s.Players(); len(got) != 1 || got[0] != (PlayerState{2, a}) {
		t.Errorf("1 уходит, 2 идёт на его место: Players = %+v, want [{2 %v}]", got, a)
	}
	// Игрок 2 уходит, игрок 1 идёт на его место: 1 применяется первым — занято.
	s = join()
	mustSubmit(t, s, Input{Tick: 2, Player: 2, Kind: Leave}, Input{Tick: 2, Player: 1, Kind: Move, DX: 1})
	s.Step()
	if got := s.Players(); len(got) != 1 || got[0] != (PlayerState{1, a}) {
		t.Errorf("2 уходит, 1 идёт на его место: Players = %+v, want [{1 %v}]", got, a)
	}
	// У одного игрока: Join раньше Move, Move раньше Leave.
	dx := int32(1) // a.X+1 < 4: справа от a был игрок 2
	s = NewSim(seed, 4)
	mustSubmit(t, s, Input{Tick: 1, Player: 1, Kind: Move, DX: dx}, Input{Tick: 1, Player: 1, Kind: Join})
	s.Step()
	if got := s.Players(); len(got) != 1 || got[0] != (PlayerState{1, Pos{a.X + dx, a.Z}}) {
		t.Errorf("Join и Move в одном тике: Players = %+v, want [{1 %v}]", got, Pos{a.X + dx, a.Z})
	}
	mustSubmit(t, s, Input{Tick: 2, Player: 1, Kind: Leave}, Input{Tick: 2, Player: 1, Kind: Move, DX: -1})
	s.Step()
	if got := s.Players(); len(got) != 0 {
		t.Errorf("Move и Leave в одном тике: Players = %+v, want []", got)
	}
}

func TestHiddenInvalidMoves(t *testing.T) {
	s := NewSim(4, 3)
	mustSubmit(t, s, Input{Tick: 1, Player: 7, Kind: Join})
	s.Step()
	ps := s.Players()
	if len(ps) != 1 {
		t.Fatalf("Players = %+v, want одного игрока", ps)
	}
	start := ps[0].Pos
	mustSubmit(t, s,
		Input{Tick: 2, Player: 7, Kind: Move, DX: 2},
		Input{Tick: 3, Player: 7, Kind: Move, DZ: -2},
		Input{Tick: 4, Player: 8, Kind: Move, DX: 1}, // нет такого игрока
		Input{Tick: 5, Player: 8, Kind: Leave},
		Input{Tick: 6, Player: 7, Kind: Join}, // уже в мире
		Input{Tick: 7, Player: 7, Kind: Move}, // шаг (0, 0)
	)
	for range 7 {
		s.Step()
	}
	if got := s.Players(); len(got) != 1 || got[0] != (PlayerState{7, start}) {
		t.Fatalf("Players = %+v, want [{7 %v}]", got, start)
	}
	// Три шага влево в мире шириной 3: последний — за край, игнорируется.
	for range 3 {
		mustSubmit(t, s, Input{Tick: s.Tick() + 1, Player: 7, Kind: Move, DX: -1})
		s.Step()
	}
	if got := s.Players()[0].Pos; got != (Pos{0, start.Z}) {
		t.Errorf("после трёх шагов влево из %v: %v, want %v", start, got, Pos{0, start.Z})
	}
	// Ввод на будущий тик ждёт своего тика.
	mustSubmit(t, s, Input{Tick: s.Tick() + 3, Player: 7, Kind: Leave})
	s.Step()
	s.Step()
	if len(s.Players()) != 1 {
		t.Fatal("ввод на будущий тик применён раньше времени")
	}
	s.Step()
	if len(s.Players()) != 0 {
		t.Fatal("ввод на будущий тик не применён")
	}
}

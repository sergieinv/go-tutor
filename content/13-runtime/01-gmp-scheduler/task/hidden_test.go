package task

import (
	"fmt"
	"slices"
	"testing"
)

func TestGlobalCheckedFirstOnTickZero(t *testing.T) {
	s := newSched(1)
	s.Spawn(0, "L")
	s.Global = []string{"X"}
	// Tick == 0: 0 % 61 == 0 — глобальная очередь раньше runnext.
	got := drain(t, s, 0)
	if !slices.Equal(got, []string{"X", "L"}) {
		t.Fatalf("порядок %v, want [X L]: при Tick %% GlobalEvery == 0 глобальная очередь проверяется первой", got)
	}
}

func TestGlobalFairness(t *testing.T) {
	s := &Sched{P: make([]P, 1), LocalCap: 256, GlobalEvery: 3}
	s.P[0].Tick = 1
	s.P[0].Local = []string{"a", "b", "c", "d", "e"}
	s.Global = []string{"X", "Y"}
	var got []string
	for range 4 {
		g, _ := s.FindRunnable(0)
		got = append(got, g)
	}
	// Tick 1 → a, Tick 2 → b, Tick 3 (3 % 3 == 0) → X из глобальной, Tick 4 → c.
	want := []string{"a", "b", "X", "c"}
	if !slices.Equal(got, want) {
		t.Fatalf("порядок %v, want %v", got, want)
	}
	if s.P[0].Tick != 5 {
		t.Errorf("Tick = %d, want 5", s.P[0].Tick)
	}
}

func TestPingPongDoesNotStarveGlobal(t *testing.T) {
	// Две горутины порождают друг друга через runnext; глобальная всё равно дождётся.
	s := &Sched{P: make([]P, 1), LocalCap: 256, GlobalEvery: 61}
	s.P[0].Tick = 1
	s.Global = []string{"starving"}
	s.Spawn(0, "ping")
	for step := 1; step <= 100; step++ {
		g, ok := s.FindRunnable(0)
		if !ok {
			t.Fatalf("шаг %d: работы нет", step)
		}
		if g == "starving" {
			if step != 61 {
				t.Fatalf("глобальная горутина запущена на шаге %d, want 61", step)
			}
			return
		}
		if g == "ping" {
			s.Spawn(0, "pong")
		} else {
			s.Spawn(0, "ping")
		}
	}
	t.Fatal("горутина из глобальной очереди не запустилась за 100 шагов")
}

func TestLocalOverflowGoesToGlobal(t *testing.T) {
	s := &Sched{P: make([]P, 1), LocalCap: 4, GlobalEvery: 61}
	for i := 1; i <= 6; i++ {
		s.Spawn(0, fmt.Sprintf("G%d", i))
	}
	// После G5: RunNext=G5, Local=[G1 G2 G3 G4] — полна.
	// G6 вытесняет G5; места нет: G1, G2 (половина с головы) и G5 — в глобальную.
	if s.P[0].RunNext != "G6" {
		t.Fatalf("RunNext = %q, want G6", s.P[0].RunNext)
	}
	if !slices.Equal(s.P[0].Local, []string{"G3", "G4"}) {
		t.Fatalf("Local = %v, want [G3 G4]", s.P[0].Local)
	}
	if !slices.Equal(s.Global, []string{"G1", "G2", "G5"}) {
		t.Fatalf("Global = %v, want [G1 G2 G5]", s.Global)
	}
}

func TestGlobalBatchLimits(t *testing.T) {
	tests := []struct {
		name      string
		nP        int
		localCap  int
		global    int
		wantLocal int // сколько ушло в локальную очередь
	}{
		{"одна горутина", 4, 256, 1, 0},
		{"делится на число P", 4, 256, 10, 2},      // min(10, 10/4+1, 128) = 3
		{"ограничение LocalCap/2", 1, 4, 10, 1},    // min(10, 11, 2) = 2
		{"ограничение длиной", 1, 256, 3, 2},       // min(3, 4, 128) = 3
		{"один P, много работы", 1, 256, 300, 127}, // min(300, 301, 128) = 128
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sched{P: make([]P, tt.nP), LocalCap: tt.localCap, GlobalEvery: 61}
			s.P[0].Tick = 1
			for i := range tt.global {
				s.Global = append(s.Global, fmt.Sprintf("g%d", i))
			}
			g, ok := s.FindRunnable(0)
			if !ok || g != "g0" {
				t.Fatalf("FindRunnable = (%q, %v), want (g0, true)", g, ok)
			}
			if len(s.P[0].Local) != tt.wantLocal {
				t.Fatalf("в локальную очередь ушло %d, want %d", len(s.P[0].Local), tt.wantLocal)
			}
			if len(s.Global) != tt.global-1-tt.wantLocal {
				t.Fatalf("в глобальной осталось %d, want %d", len(s.Global), tt.global-1-tt.wantLocal)
			}
			for i, g := range s.P[0].Local {
				if want := fmt.Sprintf("g%d", i+1); g != want {
					t.Fatalf("Local[%d] = %q, want %q: порядок глобальной очереди сохраняется", i, g, want)
				}
			}
		})
	}
}

func TestStealOddAndSingle(t *testing.T) {
	s := newSched(2)
	s.P[0].Local = []string{"a", "b", "c"}
	g, _ := s.FindRunnable(1)
	// 3 - 3/2 = 2: украдены a, b; запускается b.
	if g != "b" || !slices.Equal(s.P[1].Local, []string{"a"}) || !slices.Equal(s.P[0].Local, []string{"c"}) {
		t.Fatalf("кража из 3: запущена %q, P1.Local=%v, P0.Local=%v; want b, [a], [c]", g, s.P[1].Local, s.P[0].Local)
	}

	s = newSched(2)
	s.P[0].Local = []string{"only"}
	g, _ = s.FindRunnable(1)
	if g != "only" || len(s.P[1].Local) != 0 || len(s.P[0].Local) != 0 {
		t.Fatalf("кража из 1: запущена %q, P1.Local=%v, P0.Local=%v; want only, [], []", g, s.P[1].Local, s.P[0].Local)
	}
}

func TestStealVictimOrder(t *testing.T) {
	s := newSched(4)
	s.P[0].Local = []string{"p0"}
	s.P[1].Local = []string{"p1"}
	s.P[3].Local = []string{"p3"}
	// Поиск с P2: сначала P3, потом P0 (по кругу), P1.
	var got []string
	for range 3 {
		g, ok := s.FindRunnable(2)
		if !ok {
			t.Fatal("работа кончилась раньше времени")
		}
		got = append(got, g)
	}
	if want := []string{"p3", "p0", "p1"}; !slices.Equal(got, want) {
		t.Fatalf("порядок кражи %v, want %v (соседи p+1, p+2, … по кругу)", got, want)
	}
}

func TestStealRunNextIsLastResort(t *testing.T) {
	s := newSched(3)
	s.P[1].RunNext = "next1"
	s.P[2].Local = []string{"local2"}
	g, _ := s.FindRunnable(0)
	if g != "local2" {
		t.Fatalf("первая кража взяла %q, want local2: чужой runnext — только когда очереди всех соседей пусты", g)
	}
	g, _ = s.FindRunnable(0)
	if g != "next1" {
		t.Fatalf("вторая кража взяла %q, want next1", g)
	}
	if s.P[1].RunNext != "" {
		t.Errorf("P1.RunNext = %q после кражи, want пусто", s.P[1].RunNext)
	}
	if _, ok := s.FindRunnable(0); ok {
		t.Error("работы больше нет, а FindRunnable вернул ok")
	}
}

func TestOrderOfSources(t *testing.T) {
	s := newSched(2)
	s.P[0].Tick = 1
	s.P[0].RunNext = "next"
	s.P[0].Local = []string{"local"}
	s.Global = []string{"global"}
	s.P[1].Local = []string{"victim"}
	got := []string{}
	for range 4 {
		g, _ := s.FindRunnable(0)
		got = append(got, g)
	}
	if want := []string{"next", "local", "global", "victim"}; !slices.Equal(got, want) {
		t.Fatalf("порядок источников %v, want %v", got, want)
	}
}

func TestGosched(t *testing.T) {
	s := newSched(1)
	s.P[0].Tick = 1
	s.Global = []string{"A"}
	s.Gosched("B")
	s.Gosched("C")
	if !slices.Equal(s.Global, []string{"A", "B", "C"}) {
		t.Fatalf("Global = %v, want [A B C]: Gosched ставит в хвост", s.Global)
	}
	if s.P[0].RunNext != "" || len(s.P[0].Local) != 0 {
		t.Fatalf("Gosched не трогает очереди P: RunNext=%q Local=%v", s.P[0].RunNext, s.P[0].Local)
	}
}

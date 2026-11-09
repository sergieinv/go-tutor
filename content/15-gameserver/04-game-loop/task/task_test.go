package task

import (
	"context"
	"slices"
	"testing"
	"time"
)

const ms = time.Millisecond

// fakeClock — часы, которые идут только в Sleep и в «работе» тика.
type fakeClock struct {
	t      *testing.T
	now    time.Duration
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Duration { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	c.t.Helper()
	if d <= 0 {
		c.t.Errorf("Sleep(%v): спать нужно только на положительное время", d)
		return
	}
	c.sleeps = append(c.sleeps, d)
	c.now += d
}

// rig — цикл на фейковых часах; cost(n) — сколько длится тик n,
// starts — время начала каждого тика, nums — номера, с которыми вызван tick.
type rig struct {
	clk    *fakeClock
	loop   *Loop
	starts []time.Duration
	nums   []uint64
}

func newRig(t *testing.T, start time.Duration, cfg Config, cost func(n uint64) time.Duration) *rig {
	r := &rig{clk: &fakeClock{t: t, now: start}}
	r.loop = NewLoop(r.clk, cfg, func(n uint64) {
		r.starts = append(r.starts, r.clk.now)
		r.nums = append(r.nums, n)
		r.clk.now += cost(n)
	})
	return r
}

// stepUntil вызывает Step, пока не выполнено n тиков (не больше 10n раз).
func (r *rig) stepUntil(t *testing.T, n int) {
	t.Helper()
	for i := 0; len(r.starts) < n; i++ {
		if i > 10*n {
			t.Fatalf("за %d вызовов Step выполнено %d тиков из %d", i, len(r.starts), n)
		}
		r.loop.Step()
	}
}

// runWithin запускает Run и ждёт его возврата; зависший Run — провал.
func runWithin(t *testing.T, l *Loop, ctx context.Context) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- l.Run(ctx) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run не вернулся после отмены контекста")
		return nil
	}
}

func costs(def time.Duration, special map[uint64]time.Duration) func(uint64) time.Duration {
	return func(n uint64) time.Duration {
		if d, ok := special[n]; ok {
			return d
		}
		return def
	}
}

func TestLightLoad(t *testing.T) {
	r := newRig(t, 0, Config{Interval: 50 * ms, MaxCatchUp: 3, Window: 20}, costs(10*ms, nil))
	for range 4 {
		if got := r.loop.Step(); got != 1 {
			t.Fatalf("Step() = %d при лёгкой нагрузке, want 1", got)
		}
	}
	want := []time.Duration{0, 50 * ms, 100 * ms, 150 * ms}
	if !slices.Equal(r.starts, want) {
		t.Fatalf("тики начались в %v, want %v", r.starts, want)
	}
	if !slices.Equal(r.clk.sleeps, []time.Duration{40 * ms, 40 * ms, 40 * ms, 40 * ms}) {
		t.Fatalf("Sleep вызван с %v, want 4 раза по 40ms", r.clk.sleeps)
	}
	if !slices.Equal(r.nums, []uint64{1, 2, 3, 4}) {
		t.Fatalf("номера тиков %v", r.nums)
	}
	want2 := Stats{Ticks: 4, MSPT: 10 * ms, MaxMSPT: 10 * ms}
	if s := r.loop.Stats(); s != want2 {
		t.Fatalf("Stats = %+v, want %+v", s, want2)
	}
}

func TestCatchUp(t *testing.T) {
	r := newRig(t, 0, Config{Interval: 50 * ms, MaxCatchUp: 3, Window: 20}, costs(10*ms, map[uint64]time.Duration{3: 130 * ms}))
	var steps []int
	for i := 0; len(r.starts) < 7; i++ {
		if i > 20 {
			t.Fatalf("за 20 вызовов Step выполнено %d тиков из 7", len(r.starts))
		}
		steps = append(steps, r.loop.Step())
	}
	want := []time.Duration{0, 50 * ms, 100 * ms, 230 * ms, 240 * ms, 250 * ms, 300 * ms}
	if !slices.Equal(r.starts, want) {
		t.Fatalf("тики начались в %v, want %v", r.starts, want)
	}
	if !slices.Equal(steps, []int{1, 1, 1, 2, 1, 1}) {
		t.Fatalf("Step() вернул %v, want [1 1 1 2 1 1]", steps)
	}
	s := r.loop.Stats()
	if s.Ticks != 7 || s.Skipped != 0 || s.MaxMSPT != 130*ms || s.MSPT != (6*10+130)*ms/7 {
		t.Fatalf("Stats = %+v", s)
	}
}

func TestSkipWhenFarBehind(t *testing.T) {
	r := newRig(t, 0, Config{Interval: 50 * ms, MaxCatchUp: 3, Window: 20}, costs(10*ms, map[uint64]time.Duration{2: 400 * ms}))
	r.loop.Step()
	r.loop.Step() // тик 2 длится 400 мс
	if got := r.loop.Step(); got != 3 {
		t.Fatalf("после паузы Step() = %d, want 3 (MaxCatchUp)", got)
	}
	want := []time.Duration{0, 50 * ms, 450 * ms, 460 * ms, 470 * ms}
	if !slices.Equal(r.starts, want) {
		t.Fatalf("тики начались в %v, want %v", r.starts, want)
	}
	if s := r.loop.Stats(); s.Skipped != 5 || s.Ticks != 5 {
		t.Fatalf("Stats = %+v, want Ticks 5, Skipped 5", s)
	}
	r.loop.Step()
	if r.starts[5] != 500*ms {
		t.Fatalf("после догоняния тик начался в %v, want 500ms — снова по сетке", r.starts[5])
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := newRig(t, 0, Config{}, func(n uint64) time.Duration {
		if n == 5 {
			cancel()
		}
		return ms
	})
	if err := runWithin(t, r.loop, ctx); err != context.Canceled {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if s := r.loop.Stats(); s.Ticks != 5 {
		t.Fatalf("Ticks = %d, want 5", s.Ticks)
	}
}

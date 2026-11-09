package task

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// Нулевой Config: тик 50 мс, догонять по одному, окно 20.
func TestHiddenDefaults(t *testing.T) {
	r := newRig(t, 0, Config{}, costs(5*ms, map[uint64]time.Duration{2: 120 * ms}))
	r.stepUntil(t, 5)
	// тик 2: 50 → 170; наступили 100 и 150, догоняем один (150), 100 отброшен.
	want := []time.Duration{0, 50 * ms, 170 * ms, 200 * ms, 250 * ms}
	if !slices.Equal(r.starts, want) {
		t.Fatalf("тики начались в %v, want %v (Interval по умолчанию 50ms, MaxCatchUp 1)", r.starts, want)
	}
	if s := r.loop.Stats(); s.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", s.Skipped)
	}

	// Окно по умолчанию — 20 тиков: тик 2 (120 мс) выпадает из него
	// после 21-го тика.
	r.stepUntil(t, 21)
	if s := r.loop.Stats(); s.MaxMSPT != 120*ms {
		t.Fatalf("после 21 тика MaxMSPT = %v, want 120ms: тик 2 ещё в окне из 20", s.MaxMSPT)
	}
	r.loop.Step()
	if s := r.loop.Stats(); s.MaxMSPT != 5*ms || s.MSPT != 5*ms {
		t.Fatalf("после 22 тиков Stats = %+v, want MSPT и MaxMSPT 5ms: тик 2 вышел из окна", s)
	}
}

// Отрицательные значения — тоже значения по умолчанию.
func TestHiddenNegativeConfig(t *testing.T) {
	r := newRig(t, 0, Config{Interval: -time.Second, MaxCatchUp: -3, Window: -1}, costs(ms, nil))
	r.loop.Step()
	r.loop.Step()
	if !slices.Equal(r.starts, []time.Duration{0, 50 * ms}) {
		t.Fatalf("тики начались в %v, want [0s 50ms]", r.starts)
	}
}

// Первый тик — в момент NewLoop, сетка от него, а не от нуля.
func TestHiddenStartOffset(t *testing.T) {
	r := newRig(t, 1234*ms, Config{Interval: 100 * ms, MaxCatchUp: 2, Window: 4}, costs(30*ms, nil))
	for range 3 {
		r.loop.Step()
	}
	want := []time.Duration{1234 * ms, 1334 * ms, 1434 * ms}
	if !slices.Equal(r.starts, want) {
		t.Fatalf("тики начались в %v, want %v", r.starts, want)
	}
}

// Сон оборвался раньше времени — следующий Step не выполняет тик,
// а досыпает.
type earlyClock struct{ fakeClock }

func (c *earlyClock) Sleep(d time.Duration) {
	if d <= 0 {
		c.t.Errorf("Sleep(%v): спать нужно только на положительное время", d)
		return
	}
	c.sleeps = append(c.sleeps, d)
	c.now += d / 2
}

func TestHiddenNotDueYet(t *testing.T) {
	clk := &earlyClock{fakeClock{t: t}}
	var starts []time.Duration
	l := NewLoop(clk, Config{Interval: 40 * ms, MaxCatchUp: 3, Window: 5}, func(uint64) {
		starts = append(starts, clk.now)
	})
	if got := l.Step(); got != 1 {
		t.Fatalf("первый Step() = %d, want 1", got)
	}
	if got := l.Step(); got != 0 {
		t.Fatalf("Step() до наступления тика = %d, want 0", got)
	}
	want := []time.Duration{40 * ms, 20 * ms}
	if !slices.Equal(clk.sleeps, want) {
		t.Fatalf("Sleep вызван с %v, want %v — спать до next - Now()", clk.sleeps, want)
	}
	if len(starts) != 1 {
		t.Fatalf("тиков %d, want 1", len(starts))
	}
}

// Тик ровно в бюджет: сна нет (Sleep(0) не вызывается), тики идут
// подряд по сетке.
func TestHiddenExactBudget(t *testing.T) {
	r := newRig(t, 0, Config{Interval: 50 * ms, MaxCatchUp: 3, Window: 3}, costs(50*ms, nil))
	var steps []int
	for range 4 {
		steps = append(steps, r.loop.Step())
	}
	if len(r.clk.sleeps) != 0 {
		t.Fatalf("Sleep вызван с %v, want ни разу", r.clk.sleeps)
	}
	if !slices.Equal(steps, []int{1, 1, 1, 1}) || !slices.Equal(r.starts, []time.Duration{0, 50 * ms, 100 * ms, 150 * ms}) {
		t.Fatalf("Step() = %v, начала %v", steps, r.starts)
	}
}

// Окно меньше числа тиков и целочисленное среднее.
func TestHiddenWindowAverage(t *testing.T) {
	durs := map[uint64]time.Duration{1: 1 * ms, 2: 2 * ms, 3: 4 * ms, 4: 8 * ms, 5: 3 * ms}
	r := newRig(t, 0, Config{Interval: 50 * ms, MaxCatchUp: 1, Window: 3}, costs(0, durs))
	if s := r.loop.Stats(); s != (Stats{}) {
		t.Fatalf("до первого тика Stats = %+v, want нули", s)
	}
	r.loop.Step()
	r.loop.Step()
	if s := r.loop.Stats(); s.MSPT != 1500*time.Microsecond || s.MaxMSPT != 2*ms {
		t.Fatalf("после 2 тиков Stats = %+v, want MSPT 1.5ms (среднее по 2), MaxMSPT 2ms", s)
	}
	for range 3 {
		r.loop.Step()
	}
	// окно — тики 3, 4, 5: (4 + 8 + 3) / 3 = 5 мс
	want := Stats{Ticks: 5, MSPT: 5 * ms, MaxMSPT: 8 * ms}
	if s := r.loop.Stats(); s != want {
		t.Fatalf("Stats = %+v, want %+v", s, want)
	}

	r2 := newRig(t, 0, Config{Interval: 50 * ms, MaxCatchUp: 1, Window: 2}, costs(0, map[uint64]time.Duration{1: 1, 2: 2}))
	r2.loop.Step()
	r2.loop.Step()
	if s := r2.loop.Stats(); s.MSPT != 1 {
		t.Fatalf("MSPT = %d нс, want 1 нс: (1 + 2) / 2 с отбрасыванием остатка", s.MSPT)
	}
}

// Номера тиков идут подряд, даже когда тики отбрасываются.
func TestHiddenNumbersAfterSkip(t *testing.T) {
	r := newRig(t, 0, Config{Interval: 50 * ms, MaxCatchUp: 1, Window: 5}, costs(ms, map[uint64]time.Duration{1: time.Second}))
	r.loop.Step()
	if got := r.loop.Step(); got != 1 {
		t.Fatalf("Step() = %d, want 1", got)
	}
	r.loop.Step()
	if !slices.Equal(r.nums, []uint64{1, 2, 3}) {
		t.Fatalf("номера тиков %v, want [1 2 3]", r.nums)
	}
	// тик 1: 0 → 1000; наступили 50…1000 (20 тиков), выполнен один.
	if s := r.loop.Stats(); s.Skipped != 19 {
		t.Fatalf("Skipped = %d, want 19", s.Skipped)
	}
	if r.starts[2] != 1050*ms {
		t.Fatalf("третий тик в %v, want 1050ms", r.starts[2])
	}
}

// Run с уже отменённым контекстом не выполняет ни одного тика и не спит.
func TestHiddenRunCanceledBefore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := newRig(t, 0, Config{}, costs(ms, nil))
	if err := runWithin(t, r.loop, ctx); err != context.Canceled {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if len(r.starts) != 0 || len(r.clk.sleeps) != 0 {
		t.Fatalf("тиков %d, снов %d, want 0 и 0", len(r.starts), len(r.clk.sleeps))
	}
}

// Run возвращает ошибку контекста: дедлайн — DeadlineExceeded.
func TestHiddenRunDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	ctx2, cancel2 := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel2()
	r := newRig(t, 0, Config{}, costs(ms, nil))
	if err := runWithin(t, r.loop, ctx2); err != context.DeadlineExceeded {
		t.Fatalf("Run = %v, want context.DeadlineExceeded", err)
	}
}

// Свойство: на случайных длительностях тиков (фиксированное зерно)
// расписание совпадает с эталонной моделью из условия.
func TestHiddenRandomSchedule(t *testing.T) {
	rng := rand.New(rand.NewPCG(15, 4))
	for trial := range 30 {
		interval := time.Duration(20+rng.IntN(80)) * ms
		maxCatch := 1 + rng.IntN(4)
		window := 1 + rng.IntN(10)
		durs := make([]time.Duration, 200)
		for i := range durs {
			switch rng.IntN(10) {
			case 0:
				durs[i] = time.Duration(rng.IntN(int(5 * interval)))
			default:
				durs[i] = time.Duration(rng.IntN(int(interval)))
			}
		}
		cost := func(n uint64) time.Duration { return durs[(n-1)%uint64(len(durs))] }
		r := newRig(t, 0, Config{Interval: interval, MaxCatchUp: maxCatch, Window: window}, cost)
		m := model{interval: interval, maxCatch: maxCatch, window: window, cost: cost}
		for step := range 150 {
			got := r.loop.Step()
			want := m.step()
			if got != want {
				t.Fatalf("испытание %d, шаг %d: Step() = %d, want %d", trial, step, got, want)
			}
		}
		if !slices.Equal(r.starts, m.starts) {
			t.Fatalf("испытание %d: расписание тиков расходится с моделью", trial)
		}
		if s, w := r.loop.Stats(), m.stats(); s != w {
			t.Fatalf("испытание %d: Stats = %+v, want %+v", trial, s, w)
		}
	}
}

// model — эталон из условия, записанный в лоб.
type model struct {
	interval time.Duration
	maxCatch int
	window   int
	cost     func(uint64) time.Duration

	now, next time.Duration
	starts    []time.Duration
	durs      []time.Duration
	skipped   uint64
}

func (m *model) step() int {
	due := 0
	for t := m.next; t <= m.now; t += m.interval {
		due++
	}
	if due > m.maxCatch {
		m.skipped += uint64(due - m.maxCatch)
		m.next += time.Duration(due-m.maxCatch) * m.interval
		due = m.maxCatch
	}
	for range due {
		m.starts = append(m.starts, m.now)
		d := m.cost(uint64(len(m.starts)))
		m.durs = append(m.durs, d)
		m.now += d
		m.next += m.interval
	}
	if m.next > m.now {
		m.now = m.next
	}
	return due
}

func (m *model) stats() Stats {
	s := Stats{Ticks: uint64(len(m.durs)), Skipped: m.skipped}
	last := m.durs[max(0, len(m.durs)-m.window):]
	if len(last) == 0 {
		return s
	}
	var sum time.Duration
	for _, d := range last {
		sum += d
		s.MaxMSPT = max(s.MaxMSPT, d)
	}
	s.MSPT = sum / time.Duration(len(last))
	return s
}

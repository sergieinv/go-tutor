package task

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// probe строит n задач, которые замеряют пиковое число одновременно
// работающих. Каждая задача ждёт, пока одновременно не запустятся
// target задач (но не дольше 2 секунд), — так видно, что ограничение
// используется полностью.
type probe struct {
	cur, peak, done atomic.Int32
	failed          atomic.Bool
	reached         chan struct{}
	once            sync.Once
}

func newProbe(n, target int) (*probe, []func()) {
	p := &probe{reached: make(chan struct{})}
	tasks := make([]func(), n)
	for i := range tasks {
		tasks[i] = func() {
			c := p.cur.Add(1)
			for {
				old := p.peak.Load()
				if c <= old || p.peak.CompareAndSwap(old, c) {
					break
				}
			}
			if int(c) >= target {
				p.once.Do(func() { close(p.reached) })
			}
			if !p.failed.Load() {
				select {
				case <-p.reached:
				case <-time.After(2 * time.Second):
					p.failed.Store(true)
				}
			}
			p.cur.Add(-1)
			p.done.Add(1)
		}
	}
	return p, tasks
}

func (p *probe) check(t *testing.T, n, limit, target int) {
	t.Helper()
	if d := p.done.Load(); d != int32(n) {
		t.Errorf("завершилось %d задач из %d к моменту возврата", d, n)
	}
	if pk := p.peak.Load(); pk > int32(limit) {
		t.Errorf("одновременно работало %d задач, лимит %d", pk, limit)
	}
	if p.failed.Load() {
		t.Errorf("одновременно так и не запустилось %d задач: лимит используется не полностью", target)
	}
}

func TestRunLimited(t *testing.T) {
	tests := []struct {
		name     string
		n, limit int
	}{
		{"лимит 3 на 12 задач", 12, 3},
		{"лимит 1 — по одной", 5, 1},
		{"лимит больше числа задач", 4, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := min(tt.n, tt.limit)
			p, tasks := newProbe(tt.n, target)
			RunLimited(tasks, tt.limit)
			p.check(t, tt.n, tt.limit, target)
		})
	}
}

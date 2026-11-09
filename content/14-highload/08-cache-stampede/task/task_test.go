package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// source — фейковая база: считает вызовы, умеет «зависать» до команды.
type source struct {
	mu      sync.Mutex
	calls   int
	gate    chan struct{} // не nil — загрузка ждёт, пока его закроют
	started chan string   // сигнал о каждом начале загрузки
	fail    error         // не nil — загрузка возвращает эту ошибку
	ctxs    []context.Context
}

func newSource() *source { return &source{started: make(chan string, 1000)} }

func (s *source) Load(ctx context.Context, sku string) (Product, error) {
	s.mu.Lock()
	s.calls++
	n, gate, fail := s.calls, s.gate, s.fail
	s.ctxs = append(s.ctxs, ctx)
	s.mu.Unlock()
	s.started <- sku
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return Product{}, ctx.Err()
		}
	}
	if fail != nil {
		return Product{}, fail
	}
	return Product{SKU: sku, Name: "товар " + sku, Price: int64(100 * n)}, nil
}

func (s *source) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// hold — следующие загрузки ждут release.
func (s *source) hold() (release func()) {
	g := make(chan struct{})
	s.mu.Lock()
	s.gate = g
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.gate = nil
		s.mu.Unlock()
		close(g)
	}
}

type result struct {
	p   Product
	err error
}

func goGet(c *Cache, ctx context.Context, sku string) chan result {
	ch := make(chan result, 1)
	go func() {
		p, err := c.Get(ctx, sku)
		ch <- result{p, err}
	}()
	return ch
}

// waitLoad ждёт начала загрузки; если Get вернулся раньше — ошибка.
func waitLoad(t *testing.T, s *source, got chan result) {
	t.Helper()
	select {
	case <-s.started:
	case r := <-got:
		t.Fatalf("Get вернул %+v, %v, не дождавшись загрузки", r.p, r.err)
	case <-time.After(2 * time.Second):
		t.Fatal("загрузка не началась за 2 с")
	}
}

func recv(t *testing.T, ch chan result) result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("Get не вернулся за 2 с")
		return result{}
	}
}

func TestFreshHit(t *testing.T) {
	clk, src := newClock(), newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: clk.Now})
	p, err := c.Get(context.Background(), "apple")
	if err != nil || p.Price != 100 || p.SKU != "apple" {
		t.Fatalf("Get = %+v, %v; want товар apple за 100", p, err)
	}
	clk.Advance(59 * time.Second)
	if p, _ := c.Get(context.Background(), "apple"); p.Price != 100 || src.Calls() != 1 {
		t.Fatalf("свежая запись: Get = %+v, загрузок %d; want цена 100 и 1 загрузка", p, src.Calls())
	}
	clk.Advance(time.Second) // ровно TTL — запись устарела
	if p, _ := c.Get(context.Background(), "apple"); p.Price != 200 || src.Calls() != 2 {
		t.Errorf("устаревшая запись: Get = %+v, загрузок %d; want цена 200 и 2 загрузки", p, src.Calls())
	}
}

// 20 горутин за одним устаревшим ключом — одна загрузка на всех.
func TestStampede(t *testing.T) {
	clk, src := newClock(), newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: clk.Now})
	if p, err := c.Get(context.Background(), "apple"); err != nil || p.SKU != "apple" {
		t.Fatalf("первая загрузка: Get = %+v, %v", p, err)
	}
	<-src.started
	clk.Advance(time.Hour)
	release := src.hold()
	var chans []chan result
	for range 20 {
		chans = append(chans, goGet(c, context.Background(), "apple"))
	}
	waitLoad(t, src, chans[0])
	// Пауза не нужна правильному решению: она даёт наивному успеть
	// начать лишние загрузки, чтобы тест их поймал.
	time.Sleep(20 * time.Millisecond)
	release()
	for i, ch := range chans {
		if r := recv(t, ch); r.err != nil || r.p.Price != 200 {
			t.Errorf("горутина %d: Get = %+v, %v; want цена 200", i, r.p, r.err)
		}
	}
	if n := src.Calls(); n != 2 {
		t.Errorf("загрузок %d, want 2: первая и одна общая на 20 горутин", n)
	}
}

func TestErrorNotCached(t *testing.T) {
	clk, src := newClock(), newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: clk.Now})
	boom := errors.New("db timeout")
	src.fail = boom
	if _, err := c.Get(context.Background(), "pear"); !errors.Is(err, boom) {
		t.Fatalf("Get = %v, want ошибку загрузки", err)
	}
	src.fail = nil
	p, err := c.Get(context.Background(), "pear")
	if err != nil || src.Calls() != 2 {
		t.Errorf("после ошибки: Get = %+v, %v, загрузок %d; want товар и 2 загрузки", p, err, src.Calls())
	}
}

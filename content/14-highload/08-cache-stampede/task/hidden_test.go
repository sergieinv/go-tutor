package task

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 100 горутин за одним истёкшим ключом дают ровно одно обращение к источнику.
func TestHiddenStampede100(t *testing.T) {
	clk, src := newClock(), newSource()
	c := New(src.Load, Config{TTL: 30 * time.Second, Now: clk.Now})
	if p, err := c.Get(context.Background(), "sku-1"); err != nil || p.SKU != "sku-1" {
		t.Fatalf("первая загрузка: Get = %+v, %v", p, err)
	}
	<-src.started
	clk.Advance(30 * time.Second)
	release := src.hold()
	var entered sync.WaitGroup
	chans := make([]chan result, 100)
	for i := range chans {
		entered.Add(1)
		ch := make(chan result, 1)
		chans[i] = ch
		go func() {
			entered.Done()
			p, err := c.Get(context.Background(), "sku-1")
			ch <- result{p, err}
		}()
	}
	entered.Wait()
	waitLoad(t, src, chans[0])
	// Ждём, не начнутся ли лишние загрузки (у правильного решения — нет).
	select {
	case <-src.started:
	case <-time.After(50 * time.Millisecond):
	}
	release()
	for i, ch := range chans {
		r := recv(t, ch)
		if r.err != nil || r.p.Price != 200 || r.p.SKU != "sku-1" {
			t.Fatalf("горутина %d: Get = %+v, %v; want sku-1 за 200", i, r.p, r.err)
		}
	}
	if n := src.Calls(); n != 2 {
		t.Fatalf("обращений к источнику %d, want 2 (первая загрузка и одна на 100 горутин)", n)
	}
	// Новое значение закешировано.
	if p, _ := c.Get(context.Background(), "sku-1"); p.Price != 200 || src.Calls() != 2 {
		t.Errorf("после загрузки Get = %+v, обращений %d; want 200 из кеша", p, src.Calls())
	}
}

// Ошибка загрузки — общая для всех ждущих и не кешируется.
func TestHiddenSharedError(t *testing.T) {
	src := newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: newClock().Now})
	boom := errors.New("db: too many connections")
	src.fail = boom
	release := src.hold()
	chans := make([]chan result, 30)
	for i := range chans {
		chans[i] = goGet(c, context.Background(), "plum")
	}
	waitLoad(t, src, chans[0])
	time.Sleep(20 * time.Millisecond)
	release()
	for i, ch := range chans {
		if r := recv(t, ch); !errors.Is(r.err, boom) {
			t.Fatalf("горутина %d: err = %v, want общую ошибку загрузки", i, r.err)
		}
	}
	if n := src.Calls(); n != 1 {
		t.Fatalf("обращений %d, want 1: ошибка — одна на всех ждущих", n)
	}
	src.mu.Lock()
	src.fail = nil
	src.mu.Unlock()
	if p, err := c.Get(context.Background(), "plum"); err != nil || src.Calls() != 2 || p.SKU != "plum" {
		t.Errorf("после ошибки Get = %+v, %v, обращений %d; want новую загрузку", p, err, src.Calls())
	}
}

// Момент загрузки — когда загрузка закончилась, а не когда началась.
func TestHiddenLoadedAtCompletion(t *testing.T) {
	clk := newClock()
	calls := 0
	c := New(func(ctx context.Context, sku string) (Product, error) {
		calls++
		clk.Advance(20 * time.Second) // медленная база
		return Product{SKU: sku, Price: int64(calls)}, nil
	}, Config{TTL: 30 * time.Second, Now: clk.Now})
	c.Get(context.Background(), "a")
	clk.Advance(29 * time.Second)
	if p, _ := c.Get(context.Background(), "a"); p.Price != 1 || calls != 1 {
		t.Fatalf("через 29 с после конца загрузки: Get = %+v, загрузок %d; want из кеша", p, calls)
	}
	clk.Advance(time.Second)
	if p, _ := c.Get(context.Background(), "a"); p.Price != 2 || calls != 2 {
		t.Errorf("через 30 с: Get = %+v, загрузок %d; want новую загрузку", p, calls)
	}
}

// Отмена ждущего: он уходит сразу, загрузка продолжается для остальных.
func TestHiddenWaiterCancel(t *testing.T) {
	src := newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: newClock().Now})
	release := src.hold()
	first := goGet(c, context.Background(), "kiwi")
	waitLoad(t, src, first)
	ctx, cancel := context.WithCancel(context.Background())
	waiter := goGet(c, ctx, "kiwi")
	time.Sleep(10 * time.Millisecond)
	cancel()
	if r := recv(t, waiter); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("отменённый ждущий: err = %v, want context.Canceled (не дожидаясь загрузки)", r.err)
	}
	release()
	if r := recv(t, first); r.err != nil || r.p.SKU != "kiwi" {
		t.Fatalf("первый вызов: Get = %+v, %v; want товар", r.p, r.err)
	}
	if n := src.Calls(); n != 1 {
		t.Errorf("обращений %d, want 1", n)
	}
}

// Отмена того, кто начал загрузку, не обрывает её: остальные получают
// результат, и он попадает в кеш.
func TestHiddenLeaderCancel(t *testing.T) {
	src := newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: newClock().Now})
	release := src.hold()
	ctx, cancel := context.WithCancel(context.Background())
	leader := goGet(c, ctx, "fig")
	waitLoad(t, src, leader)
	follower := goGet(c, context.Background(), "fig")
	time.Sleep(10 * time.Millisecond)
	cancel()
	if r := recv(t, leader); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("начавший загрузку после отмены: err = %v, want context.Canceled", r.err)
	}
	src.mu.Lock()
	loadCtx := src.ctxs[0]
	src.mu.Unlock()
	if loadCtx.Err() != nil {
		t.Fatalf("контекст загрузки отменён вместе с вызовом: %v", loadCtx.Err())
	}
	release()
	if r := recv(t, follower); r.err != nil || r.p.SKU != "fig" {
		t.Fatalf("второй вызов: Get = %+v, %v; want товар", r.p, r.err)
	}
	if p, err := c.Get(context.Background(), "fig"); err != nil || p.SKU != "fig" || src.Calls() != 1 {
		t.Errorf("результат не закеширован: Get = %+v, %v, обращений %d", p, err, src.Calls())
	}
}

// Загрузчик получает значения контекста вызова.
func TestHiddenLoadContextValues(t *testing.T) {
	type key struct{}
	src := newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: newClock().Now})
	ctx := context.WithValue(context.Background(), key{}, "trace-7")
	c.Get(ctx, "a")
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.ctxs) != 1 || src.ctxs[0].Value(key{}) != "trace-7" {
		t.Fatal("загрузка не вызвана или контекст загрузки потерял значения контекста вызова")
	}
	if _, ok := src.ctxs[0].Deadline(); ok {
		t.Error("LoadTimeout = 0: у контекста загрузки не должно быть дедлайна")
	}
}

func TestHiddenLoadTimeout(t *testing.T) {
	src := newSource()
	c := New(src.Load, Config{TTL: time.Minute, LoadTimeout: 30 * time.Millisecond, Now: newClock().Now})
	release := src.hold() // источник «завис»
	defer release()
	got := goGet(c, context.Background(), "slow")
	waitLoad(t, src, got)
	if r := recv(t, got); !errors.Is(r.err, context.DeadlineExceeded) {
		t.Fatalf("зависшая загрузка: err = %v, want context.DeadlineExceeded", r.err)
	}
	src.mu.Lock()
	_, ok := src.ctxs[0].Deadline()
	src.mu.Unlock()
	if !ok {
		t.Error("LoadTimeout > 0: у контекста загрузки должен быть дедлайн")
	}
}

// Отменённый заранее контекст: свежая запись отдаётся, загрузка не начинается.
func TestHiddenCanceledBeforeCall(t *testing.T) {
	clk, src := newClock(), newSource()
	c := New(src.Load, Config{TTL: time.Minute, Now: clk.Now})
	c.Get(context.Background(), "a")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, err := c.Get(ctx, "a"); err != nil || p.SKU != "a" {
		t.Fatalf("свежая запись с отменённым ctx: Get = %+v, %v; want из кеша", p, err)
	}
	clk.Advance(time.Minute)
	if _, err := c.Get(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("устаревшая запись с отменённым ctx: err = %v, want context.Canceled", err)
	}
	if n := src.Calls(); n != 1 {
		t.Errorf("обращений %d, want 1: с отменённым ctx загрузка не начинается", n)
	}
}

// Загрузка одного ключа не задерживает другие.
func TestHiddenKeysIndependent(t *testing.T) {
	var slowGate = make(chan struct{})
	var calls atomic.Int64
	c := New(func(ctx context.Context, sku string) (Product, error) {
		calls.Add(1)
		if sku == "slow" {
			<-slowGate
		}
		return Product{SKU: sku}, nil
	}, Config{TTL: time.Minute, Now: newClock().Now})
	slow := goGet(c, context.Background(), "slow")
	for i := range 5 {
		sku := fmt.Sprintf("fast-%d", i)
		r := recv(t, goGet(c, context.Background(), sku))
		if r.err != nil || r.p.SKU != sku {
			t.Fatalf("Get(%s) = %+v, %v", sku, r.p, r.err)
		}
	}
	close(slowGate)
	if r := recv(t, slow); r.p.SKU != "slow" {
		t.Errorf("Get(slow) = %+v", r.p)
	}
	if n := calls.Load(); n != 6 {
		t.Errorf("загрузок %d, want 6 — по одной на ключ", n)
	}
}

func TestHiddenRealClock(t *testing.T) {
	src := newSource()
	c := New(src.Load, Config{TTL: time.Hour})
	c.Get(context.Background(), "a")
	c.Get(context.Background(), "a")
	if n := src.Calls(); n != 1 {
		t.Errorf("Now == nil: обращений %d, want 1", n)
	}
}

// Много ключей и горутин сразу (запускайте с -race): каждый ответ — свой
// товар, загрузок не больше, чем раз на ключ на каждый сдвиг часов.
func TestHiddenConcurrentKeys(t *testing.T) {
	clk, src := newClock(), newSource()
	src.started = make(chan string, 10000)
	c := New(src.Load, Config{TTL: time.Minute, Now: clk.Now})
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for i := range 200 {
				sku := fmt.Sprintf("sku-%d", (g+i)%10)
				p, err := c.Get(context.Background(), sku)
				if err != nil || p.SKU != sku {
					t.Errorf("Get(%s) = %+v, %v", sku, p, err)
					return
				}
			}
		})
	}
	wg.Wait()
	if n := src.Calls(); n != 10 {
		t.Errorf("загрузок %d, want 10 — по одной на ключ", n)
	}
}

package main

import (
	"runtime"
	"sync"
	"testing"
)

// Стресс-тест SPSC под `-race`: производитель кладёт 0..n-1,
// потребитель обязан получить их ровно в этом порядке.
// Маленькое кольцо — чтобы часто упираться в «полно» и «пусто».
func TestSPSCOrder(t *testing.T) {
	const n = 20_000
	q := NewSPSC(8)
	go func() {
		for i := 0; i < n; {
			if q.TryPush(i) {
				i++
			} else {
				runtime.Gosched()
			}
		}
	}()
	for want := 0; want < n; {
		v, ok := q.TryPop()
		if !ok {
			runtime.Gosched()
			continue
		}
		if v != want {
			t.Fatalf("получили %d, ждали %d", v, want)
		}
		want++
	}
	t.Logf("получено %d значений по порядку", n)
}

// MPSC: четыре производителя. Общего порядка между ними нет,
// но значения одного производителя приходят в его порядке.
func TestMPSC(t *testing.T) {
	const producers, per = 4, 5000
	q := NewMPSC(16)
	var wg sync.WaitGroup
	for p := range producers {
		wg.Go(func() {
			for i := 0; i < per; {
				if q.TryPush(p*per + i) {
					i++
				} else {
					runtime.Gosched()
				}
			}
		})
	}
	next := make([]int, producers)
	for got := 0; got < producers*per; {
		v, ok := q.TryPop()
		if !ok {
			runtime.Gosched()
			continue
		}
		p, i := v/per, v%per
		if i != next[p] {
			t.Fatalf("производитель %d: получили %d, ждали %d", p, i, next[p])
		}
		next[p]++
		got++
	}
	wg.Wait()
	t.Logf("получено %d значений, порядок каждого производителя сохранён", producers*per)
}

// Бенчмарки передачи b.N значений от одной горутины другой.
// Канал внутри — тоже кольцо, но под мьютексом и с парковкой
// горутин; сравните `-cpu 1,4` и помните, что ожидание у колец
// здесь — активное (`Gosched`), оно жжёт CPU.
func BenchmarkChannel(b *testing.B) {
	ch := make(chan int, 1024)
	go func() {
		for i := range b.N {
			ch <- i
		}
	}()
	for range b.N {
		<-ch
	}
}

func BenchmarkSPSC(b *testing.B) {
	q := NewSPSC(1024)
	go func() {
		for i := 0; i < b.N; {
			if q.TryPush(i) {
				i++
			} else {
				runtime.Gosched()
			}
		}
	}()
	for got := 0; got < b.N; {
		if _, ok := q.TryPop(); ok {
			got++
		} else {
			runtime.Gosched()
		}
	}
}

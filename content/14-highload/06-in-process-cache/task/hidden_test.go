package task

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Обновление существующего ключа делает его свежим и ничего не вытесняет.
func TestHiddenUpdateRefreshes(t *testing.T) {
	clk := newClock()
	c := New[string, int](3, time.Hour, clk.Now)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)
	c.Set("a", 10) // обновление: порядок от свежих — a, c, b
	if n := c.Len(); n != 3 {
		t.Fatalf("после обновления Len = %d, want 3", n)
	}
	c.Set("d", 4) // вытесняется b
	mustMiss(t, c, "b")
	mustGet(t, c, "a", 10)
	c.Set("e", 5) // порядок a, d, c → вытесняется c
	mustMiss(t, c, "c")
	mustGet(t, c, "d", 4)
	mustGet(t, c, "e", 5)
}

// Граница срока: запись жива до now < записано + ttl.
func TestHiddenTTLBoundary(t *testing.T) {
	clk := newClock()
	c := New[string, int](4, time.Minute, clk.Now)
	c.Set("k", 1)
	clk.Advance(time.Minute - time.Nanosecond)
	mustGet(t, c, "k", 1)
	clk.Advance(time.Nanosecond)
	mustMiss(t, c, "k")
}

// Get не продлевает срок, а Set (в том числе обновление) — продлевает.
func TestHiddenGetDoesNotExtend(t *testing.T) {
	clk := newClock()
	c := New[string, int](4, time.Minute, clk.Now)
	c.Set("read", 1)
	c.Set("rewrite", 1)
	for range 5 {
		clk.Advance(10 * time.Second)
		mustGet(t, c, "read", 1)
	}
	c.Set("rewrite", 2) // на 50-й секунде: срок — до 110-й
	clk.Advance(10 * time.Second)
	mustMiss(t, c, "read")
	clk.Advance(49 * time.Second)
	mustGet(t, c, "rewrite", 2)
	clk.Advance(time.Second)
	mustMiss(t, c, "rewrite")
}

// Get истёкшего ключа удаляет запись: она перестаёт занимать место.
func TestHiddenExpiredGetRemoves(t *testing.T) {
	clk := newClock()
	c := New[string, int](2, time.Minute, clk.Now)
	c.Set("a", 1)
	clk.Advance(30 * time.Second)
	c.Set("b", 2)
	clk.Advance(10 * time.Second)
	mustGet(t, c, "a", 1) // a свежее b по использованию
	clk.Advance(20 * time.Second)
	mustMiss(t, c, "a") // истёк — удалён
	c.Set("c", 3)       // места хватает: b не вытесняется
	mustGet(t, c, "b", 2)
	mustGet(t, c, "c", 3)
}

// Переполнение вытесняет самую давно использованную запись,
// даже если какая-то другая уже истекла, но ещё лежит в кеше.
func TestHiddenEvictsLRUEvenIfOtherExpired(t *testing.T) {
	clk := newClock()
	c := New[string, int](2, time.Minute, clk.Now)
	c.Set("old", 1)
	clk.Advance(30 * time.Second)
	c.Set("fresh", 2)
	mustGet(t, c, "old", 1) // порядок: old, fresh
	clk.Advance(30 * time.Second)
	// old истёк, но лежит в кеше; самый давно использованный — fresh.
	c.Set("new", 3)
	mustMiss(t, c, "fresh")
	mustMiss(t, c, "old")
	mustGet(t, c, "new", 3)
}

func TestHiddenPurgeAndLen(t *testing.T) {
	clk := newClock()
	c := New[int, string](10, time.Minute, clk.Now)
	for i := range 6 {
		c.Set(i, fmt.Sprint(i))
		clk.Advance(10 * time.Second)
	}
	// Записаны на 0, 10, …, 50 с, сейчас 60 с: истекла только запись 0.
	if n := c.Len(); n != 5 {
		t.Fatalf("Len = %d, want 5", n)
	}
	clk.Advance(25 * time.Second) // 85 с: истекли 0, 10, 20
	if n := c.Purge(); n != 3 {
		t.Fatalf("Purge = %d, want 3", n)
	}
	if n := c.Purge(); n != 0 {
		t.Fatalf("повторный Purge = %d, want 0", n)
	}
	if n := c.Len(); n != 3 {
		t.Errorf("Len = %d, want 3", n)
	}
	for i := 3; i < 6; i++ {
		mustGet(t, c, i, fmt.Sprint(i))
	}
}

func TestHiddenDeleteMissing(t *testing.T) {
	c := New[string, int](2, 0, newClock().Now)
	c.Delete("nope")
	c.Set("a", 1)
	c.Delete("a")
	c.Delete("a")
	if n := c.Len(); n != 0 {
		t.Errorf("Len = %d, want 0", n)
	}
	c.Set("b", 2)
	c.Set("c", 3)
	mustGet(t, c, "b", 2) // удалённый a не занимает место
	mustGet(t, c, "c", 3)
}

// capacity < 1 — как 1, ttl <= 0 — записи не истекают, now == nil — time.Now.
func TestHiddenDefaults(t *testing.T) {
	clk := newClock()
	c := New[string, int](0, 0, clk.Now)
	c.Set("a", 1)
	clk.Advance(100 * 365 * 24 * time.Hour)
	mustGet(t, c, "a", 1)
	c.Set("b", 2)
	mustMiss(t, c, "a")
	mustGet(t, c, "b", 2)
	if n := c.Purge(); n != 0 {
		t.Errorf("Purge без срока = %d, want 0", n)
	}

	neg := New[string, int](-3, -time.Second, clk.Now)
	neg.Set("x", 1)
	clk.Advance(time.Hour)
	mustGet(t, neg, "x", 1)

	rc := New[string, int](2, time.Hour, nil)
	rc.Set("x", 7)
	mustGet(t, rc, "x", 7)
}

type product struct {
	SKU   string
	Price int64
}

func TestHiddenStructValues(t *testing.T) {
	c := New[int64, product](2, time.Minute, newClock().Now)
	c.Set(1, product{"apple", 120})
	mustGet(t, c, 1, product{"apple", 120})
	mustMiss(t, c, 2)
	var zero product
	if v, _ := c.Get(2); v != zero {
		t.Errorf("промах вернул %v, want нулевое значение", v)
	}
}

// Одновременные Get/Set/Delete/Len/Purge из многих горутин (запускайте с -race).
func TestHiddenConcurrent(t *testing.T) {
	clk := newClock()
	c := New[int, int](64, time.Second, clk.Now)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 2000 {
				k := (g*7 + i) % 200
				switch i % 7 {
				case 0:
					c.Delete(k)
				case 1:
					c.Len()
				case 2:
					c.Purge()
				case 3:
					clk.Advance(time.Millisecond)
				default:
					c.Set(k, k)
					if v, ok := c.Get(k); ok && v != k {
						t.Errorf("Get(%d) = %d", k, v)
					}
				}
			}
		})
	}
	wg.Wait()
	if n := c.Len(); n > 64 {
		t.Errorf("Len = %d больше ёмкости 64", n)
	}
}

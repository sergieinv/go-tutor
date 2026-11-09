// Атомики умеют больше, чем `Add`: `CompareAndSwap` позволяет
// построить любую операцию «прочитал — вычислил — записал»
// без мьютекса, а `atomic.Pointer` — публиковать неизменяемые
// снимки данных, которые читают без блокировок (copy-on-write).

package main

import (
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
)

// CAS-цикл: взять токен, только если лимит не исчерпан.
// `Add(1)` с проверкой после не годится — счётчик успел бы
// выйти за лимит. Если между `Load` и `CompareAndSwap` кто-то
// изменил значение, CAS вернёт false, и мы перечитаем.
type Limiter struct {
	used  atomic.Int64
	limit int64
}

func (l *Limiter) TryAcquire() bool {
	for {
		cur := l.used.Load()
		if cur >= l.limit {
			return false
		}
		if l.used.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

// Снимок конфигурации. После публикации его никто не меняет —
// поэтому читать поля и map можно без блокировок.
type Config struct {
	Version int
	Limits  map[string]int
}

// `Store` хранит указатель на текущий снимок. Читатель делает
// один атомарный `Load` и дальше работает со своим снимком,
// даже если писатель уже опубликовал новый.
type Store struct{ cur atomic.Pointer[Config] }

func (s *Store) Load() *Config { return s.cur.Load() }

// Писатель копирует снимок, меняет копию и публикует её CAS.
// Если за это время опубликовали другой снимок, его изменения
// не затираются: `f` вызывается заново со свежим снимком.
func (s *Store) Update(f func(old *Config) *Config) {
	for {
		old := s.cur.Load()
		if s.cur.CompareAndSwap(old, f(old)) {
			return
		}
	}
}

// Новый снимок: копия map, оба лимита и версия согласованы.
func bump(old *Config) *Config {
	limits := maps.Clone(old.Limits)
	limits["api"] = old.Version + 1
	limits["db"] = old.Version + 1
	return &Config{Version: old.Version + 1, Limits: limits}
}

func main() {
	l := &Limiter{limit: 5000}
	var granted atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				if l.TryAcquire() {
					granted.Add(1)
				}
			}
		})
	}
	wg.Wait()
	fmt.Println("выдано токенов:", granted.Load(), "из 8000 попыток")

	// Четыре писателя по 250 обновлений и четыре читателя,
	// которые проверяют, что снимок согласован.
	var s Store
	s.cur.Store(&Config{Limits: map[string]int{"api": 0, "db": 0}})
	first := s.Load()

	var done atomic.Bool
	var torn atomic.Int64
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for !done.Load() {
				c := s.Load()
				if c.Limits["api"] != c.Version || c.Limits["db"] != c.Version {
					torn.Add(1)
				}
			}
		})
	}
	var writers sync.WaitGroup
	for range 4 {
		writers.Go(func() {
			for range 250 {
				s.Update(bump)
			}
		})
	}
	writers.Wait()
	done.Store(true)
	readers.Wait()

	last := s.Load()
	fmt.Println("версия:", last.Version, "лимиты:", last.Limits["api"], last.Limits["db"])
	fmt.Println("несогласованных снимков:", torn.Load())
	fmt.Println("первый снимок не изменился:", first.Version == 0 && first.Limits["api"] == 0)
}

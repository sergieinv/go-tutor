// Ограничение частоты (rate limiting) — не больше N операций
// за интервал: чтобы не положить чужой API и уложиться
// в квоту. В Go простейший ограничитель — канал, в который
// время «капает» токены.

package main

import (
	"fmt"
	"time"
)

func main() {

	// `time.Ticker` присылает значение в канал `C` раз в
	// интервал. Ждём тик перед каждым запросом — и запросы
	// идут не чаще раза в 20 мс.
	start := time.Now()
	ticker := time.NewTicker(20 * time.Millisecond)
	for i := 1; i <= 3; i++ {
		<-ticker.C
		fmt.Println("равномерно: запрос", i)
	}
	ticker.Stop()
	fmt.Println("заняло не меньше 60 мс:", time.Since(start) >= 60*time.Millisecond)

	// Всплески (burst): буферизованный канал токенов.
	// Буфер заполнен заранее — первые три запроса проходят
	// сразу, дальше по одному на тик. Время засекаем до
	// запуска тикера, чтобы нижняя оценка была честной.
	start = time.Now()
	tokens := make(chan struct{}, 3)
	for range cap(tokens) {
		tokens <- struct{}{}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				// Неблокирующая отправка: полный буфер —
				// лишний токен выбрасываем, не копим.
				select {
				case tokens <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()

	for i := 1; i <= 5; i++ {
		<-tokens
		fmt.Println("со всплеском: запрос", i)
	}
	close(done)
	// 3 из буфера + 2 по тику: не меньше 2 × 20 мс.
	fmt.Println("заняло не меньше 40 мс:", time.Since(start) >= 40*time.Millisecond)

	// Тот же алгоритм без горутины — token bucket: токены
	// досчитываются по прошедшему времени при каждом запросе.
	// Время передаём явно, поэтому вывод не зависит от часов.
	b := bucket{tokens: 2, capacity: 2, every: 100 * time.Millisecond}
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, ms := range []int{0, 0, 0, 50, 100, 150, 500} {
		now := t0.Add(time.Duration(ms) * time.Millisecond)
		fmt.Printf("t=%3d мс: %v\n", ms, b.allow(now))
	}
}

// bucket — ведро на capacity токенов, один токен
// добавляется каждые every.
type bucket struct {
	tokens   float64
	capacity float64
	every    time.Duration
	last     time.Time
}

func (b *bucket) allow(now time.Time) bool {
	if !b.last.IsZero() {
		b.tokens += float64(now.Sub(b.last)) / float64(b.every)
		b.tokens = min(b.tokens, b.capacity) // лишнее не копится
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

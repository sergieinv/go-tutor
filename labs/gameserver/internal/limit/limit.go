// Пакет limit — token bucket на реальном времени для горутин чтения
// соединений (лимит частоты входящих пакетов). Не потокобезопасен: у
// каждого соединения свой экземпляр.
package limit

import "time"

// Bucket — rate жетонов в секунду, запас burst.
type Bucket struct {
	rate, burst float64
	tokens      float64
	last        time.Time
}

// New — полный запас на старте: всплеск пакетов при входе в игру
// (настройки клиента, подтверждения, первые перемещения) — нормален.
func New(rate, burst float64, now time.Time) *Bucket {
	return &Bucket{rate: rate, burst: burst, tokens: burst, last: now}
}

// Allow тратит жетон; false — лимит превышен.
func (b *Bucket) Allow(now time.Time) bool {
	if dt := now.Sub(b.last).Seconds(); dt > 0 {
		b.tokens = min(b.burst, b.tokens+dt*b.rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

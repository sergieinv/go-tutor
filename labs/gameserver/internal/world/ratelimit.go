package world

// bucket — token bucket, время в котором — номер тика. Мир не смотрит на
// часы: лимиты детерминированы, и тесты проверяют их без sleep.
type bucket struct {
	tokens   float64
	lastTick uint64
}

// allow тратит один жетон. perTick — пополнение за тик, burst — ёмкость.
func (b *bucket) allow(tick uint64, perTick, burst float64) bool {
	if tick > b.lastTick {
		b.tokens = min(burst, b.tokens+float64(tick-b.lastTick)*perTick)
		b.lastTick = tick
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func newBucket(tick uint64, burst float64) bucket { return bucket{tokens: burst, lastTick: tick} }

// Package hist — гистограмма задержек с логарифмически-линейными
// корзинами (как HdrHistogram, только проще): перцентили без хранения
// всех значений и без внешних зависимостей.
//
// Значения — микросекунды. До 64 мкс корзины по 1 мкс, дальше каждая
// степень двойки делится на 32 корзины: относительная ошибка перцентиля —
// не больше 1/32 ≈ 3%, а память постоянная (~2 тыс. счётчиков на весь
// диапазон до часов). Среднее и сумма задержек нарочно не считаются:
// у задержек тяжёлый хвост, и среднее его прячет.
package hist

import (
	"math"
	"math/bits"
	"time"
)

// subBits — log2 числа корзин на степень двойки.
const subBits = 5

const subCount = 1 << subBits

// Histogram — гистограмма. Нулевое значение готово к работе. Не
// потокобезопасна: каждый поток пишет в свою, потом Merge (или внешняя
// блокировка).
type Histogram struct {
	counts []uint64
	total  uint64
	max    uint64
}

// index — номер корзины для значения v.
func index(v uint64) int {
	if v < 2*subCount {
		return int(v)
	}
	e := bits.Len64(v) - subBits - 1 // v>>e в [32, 64)
	return (e+1)*subCount + int(v>>e) - subCount
}

// upper — наибольшее значение, попадающее в корзину i.
func upper(i int) uint64 {
	if i < 2*subCount {
		return uint64(i)
	}
	e := i/subCount - 1
	m := uint64(i%subCount + subCount)
	return (m+1)<<e - 1
}

// Record добавляет значение.
func (h *Histogram) Record(d time.Duration) {
	v := uint64(max(d.Microseconds(), 0))
	i := index(v)
	if i >= len(h.counts) {
		grown := make([]uint64, i+1+subCount)
		copy(grown, h.counts)
		h.counts = grown
	}
	h.counts[i]++
	h.total++
	h.max = max(h.max, v)
}

// Count — число значений.
func (h *Histogram) Count() uint64 { return h.total }

// Max — наибольшее значение (точно, не корзина).
func (h *Histogram) Max() time.Duration { return time.Duration(h.max) * time.Microsecond }

// Quantile — значение, не меньше которого q-я доля значений (0 < q ≤ 1):
// верхняя граница корзины, в которую попал ранг ceil(q·count), но не
// больше максимума. Пустая гистограмма — 0.
func (h *Histogram) Quantile(q float64) time.Duration {
	if h.total == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(h.total)))
	rank = min(max(rank, 1), h.total)
	var seen uint64
	for i, c := range h.counts {
		seen += c
		if seen >= rank {
			return time.Duration(min(upper(i), h.max)) * time.Microsecond
		}
	}
	return h.Max()
}

// Merge добавляет значения other.
func (h *Histogram) Merge(other *Histogram) {
	if len(other.counts) > len(h.counts) {
		grown := make([]uint64, len(other.counts))
		copy(grown, h.counts)
		h.counts = grown
	}
	for i, c := range other.counts {
		h.counts[i] += c
	}
	h.total += other.total
	h.max = max(h.max, other.max)
}

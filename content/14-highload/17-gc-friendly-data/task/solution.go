package task

import "math"

// Bloom — фильтр Блума: битовый массив в []uint64, без указателей
// на элементы и без хранения самих ключей.
type Bloom struct {
	bits []uint64
	m    uint64 // число бит
	k    int    // число хеш-функций
}

func NewBloom(n int, p float64) *Bloom {
	if n < 1 {
		n = 1
	}
	if !(p > 0 && p < 1) {
		p = 0.01
	}
	m := uint64(math.Ceil(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2)))
	k := max(1, int(math.Round(float64(m)/float64(n)*math.Ln2)))
	return &Bloom{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

func (b *Bloom) Bits() int { return int(b.m) }
func (b *Bloom) K() int    { return b.k }

// hash — FNV-1a 64 по байтам строки и финализатор fmix64.
// Цикл по строке не выделяет память, в отличие от fnv.New64a + Write.
func hash(key string) (h1, h2 uint64) {
	h := uint64(14695981039346656037)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h & 0xffffffff, h >> 32
}

func (b *Bloom) Add(key string) {
	h1, h2 := hash(key)
	for i := range uint64(b.k) {
		j := (h1 + i*h2) % b.m
		b.bits[j/64] |= 1 << (j % 64)
	}
}

func (b *Bloom) MayContain(key string) bool {
	h1, h2 := hash(key)
	for i := range uint64(b.k) {
		j := (h1 + i*h2) % b.m
		if b.bits[j/64]&(1<<(j%64)) == 0 {
			return false
		}
	}
	return true
}

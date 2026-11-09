package main

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
)

// Хеш для шардирования должен быть одинаковым в каждом процессе
// и на каждой машине: FNV-1a из стандартной библиотеки, а следом —
// финализатор `fmix64` из MurmurHash3. Он перемешивает биты:
// у самого FNV-1a похожие строки («S1/0», «S1/1») дают близкие
// хеши, и точки одного шарда на кольце слипаются.
func hash64(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	x := h.Sum64()
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

// Роутер строится для n шардов и отвечает на вопрос «чей ключ».
type router func(n int) func(key string) int

// Остаток от деления: равномерно, но номер зависит от n.
func byMod(n int) func(string) int {
	return func(key string) int { return int(hash64(key) % uint64(n)) }
}

// Consistent hashing: у каждого шарда `vnodes` точек на кольце
// uint64, ключ достаётся первой точке не меньше своего хеша.
func byRing(vnodes int) router {
	return func(n int) func(string) int {
		type point struct {
			h     uint64
			shard int
		}
		var ring []point
		for s := range n {
			for v := range vnodes {
				ring = append(ring, point{hash64("shard-" + strconv.Itoa(s) + "#" + strconv.Itoa(v)), s})
			}
		}
		slices.SortFunc(ring, func(a, b point) int { return cmp.Compare(a.h, b.h) })
		return func(key string) int {
			h := hash64(key)
			i, _ := slices.BinarySearchFunc(ring, h, func(p point, h uint64) int { return cmp.Compare(p.h, h) })
			if i == len(ring) {
				i = 0 // за последней точкой — снова первая
			}
			return ring[i].shard
		}
	}
}

// Rendezvous (HRW): у каждой пары «ключ, шард» свой вес,
// ключ достаётся шарду с наибольшим весом. Кольца нет,
// зато поиск — O(n) на ключ.
func byRendezvous(n int) func(string) int {
	return func(key string) int {
		best, owner := uint64(0), 0
		for s := range n {
			if w := hash64("shard-" + strconv.Itoa(s) + "/" + key); w > best {
				best, owner = w, s
			}
		}
		return owner
	}
}

// Jump consistent hash (Lamping, Veach, 2014): ни памяти, ни кольца,
// но шарды — номера 0..n-1, добавлять и убирать можно только последний.
func byJump(n int) func(string) int {
	return func(key string) int {
		k := hash64(key)
		b, j := int64(-1), int64(0)
		for j < int64(n) {
			b = j
			k = k*2862933555777941757 + 1
			j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((k>>33)+1)))
		}
		return int(b)
	}
}

// Для каждого роутера — сколько ключей из 10 000 у каждого
// из четырёх шардов и сколько ключей сменят шард, если добавить пятый.
func main() {
	keys := make([]string, 10_000)
	for i := range keys {
		keys[i] = "customer-" + strconv.Itoa(i)
	}
	routers := []struct {
		name  string
		route router
	}{
		{"hash % N", byMod},
		{"кольцо, 1 точка", byRing(1)},
		{"кольцо, 160 точек", byRing(160)},
		{"rendezvous", byRendezvous},
		{"jump hash", byJump},
	}
	for _, r := range routers {
		four, five := r.route(4), r.route(5)
		perShard := make([]int, 4)
		moved, toNew := 0, 0
		for _, k := range keys {
			before, after := four(k), five(k)
			perShard[before]++
			if before != after {
				moved++
				if after == 4 {
					toNew++
				}
			}
		}
		fmt.Printf("%-18s шарды %v  переехало %4d (%2d%%), из них в новый: %d\n",
			r.name, perShard, moved, moved*100/len(keys), toNew)
	}
}

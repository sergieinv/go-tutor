package main

import (
	"fmt"
	"runtime"
	"runtime/metrics"
	"strings"
	"unique"
)

// `/gc/scan/heap:bytes` — сколько байт кучи сборщику мусора придётся
// просматривать в поисках указателей. Объекты без указателей лежат
// в отдельных noscan-спанах, и GC их не читает вовсе: помечает
// живыми и идёт дальше.
func scannable() uint64 {
	runtime.GC()
	s := []metrics.Sample{{Name: "/gc/scan/heap:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

// growth — на сколько выросла сканируемая куча, пока жив результат build.
func growth(build func() any) (mb float64) {
	before := scannable()
	keep := build()
	after := scannable()
	runtime.KeepAlive(keep)
	return float64(int64(after)-int64(before)) / (1 << 20)
}

type item struct {
	id    int64
	price int64
}

const n = 1 << 20

// Дерево в срезе: дети — индексы, а не указатели. Узел без указателей,
// весь срез — один noscan-объект, сколько бы узлов в нём ни было.
type node struct {
	key         int64
	left, right int32 // -1 — нет ребёнка
}

func main() {
	// 1. Миллион чисел против миллиона указателей на числа.
	values := growth(func() any { return make([]int64, n) })
	pointers := growth(func() any {
		s := make([]*int64, n)
		for i := range s {
			s[i] = new(int64)
		}
		return s
	})
	fmt.Println("[]int64: сканируемая куча выросла меньше чем на 1 МБ:", values < 1)
	fmt.Println("[]*int64: сканируемая куча выросла больше чем на 4 МБ:", pointers > 4)

	// 2. Кеш «id → товар»: map со значениями-структурами без
	// указателей против map с указателями на объекты.
	byValue := growth(func() any {
		m := make(map[int64]item, n/4)
		for i := range int64(n / 4) {
			m[i] = item{id: i, price: i * 10}
		}
		return m
	})
	byPointer := growth(func() any {
		m := make(map[int64]*item, n/4)
		for i := range int64(n / 4) {
			m[i] = &item{id: i, price: i * 10}
		}
		return m
	})
	fmt.Println("map[int64]item сканируется меньше, чем map[int64]*item:", byValue < byPointer/10)

	// 3. Строки — тоже указатели, а одинаковые строки в миллионе
	// записей — миллион копий. `unique.Make` интернирует значение:
	// `Handle` — один указатель на общую каноническую копию, а
	// сравнение двух Handle — сравнение указателей. Без указателей
	// совсем — свой словарь «строка → uint32».
	a := unique.Make(strings.Repeat("ab", 3))
	b := unique.Make(string([]byte("ababab"))) // другая копия байт
	c := unique.Make(strings.ToUpper("ababab"))
	fmt.Println("равные строки — равные Handle:", a == b, "разные:", a == c, "значение:", b.Value())

	// 4. Индексы вместо указателей.
	tree := []node{{key: 50, left: 1, right: 2}, {key: 30, left: -1, right: -1}, {key: 70, left: -1, right: -1}}
	fmt.Println("найден ключ 70:", find(tree, 70), "ключ 40:", find(tree, 40))
}

func find(tree []node, key int64) bool {
	for i := int32(0); i >= 0; {
		switch n := tree[i]; {
		case key == n.key:
			return true
		case key < n.key:
			i = n.left
		default:
			i = n.right
		}
	}
	return false
}

// Interest management по чанкам: каждый игрок подписан на квадрат
// чанков (2r+1)² вокруг своего чанка. При переходе границы чанка
// сервер шлёт только разность квадратов, событие «блок изменён» —
// только подписчикам его чанка, а изменения за тик — одним пакетом
// на чанк. Всё это делает горутина-владелец мира, поэтому
// блокировок здесь нет.
package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
)

type ChunkPos struct{ X, Z int }

func (c ChunkPos) String() string { return fmt.Sprintf("(%d,%d)", c.X, c.Z) }

// Чанк — столбец 16×16 блоков. Координаты игрока — `float64`
// и бывают отрицательными: `int(x)/16` округляет к нулю и кладёт
// x = -0.5 в чанк 0. Правильно — `math.Floor`.
func chunkOf(x, z float64) ChunkPos {
	return ChunkPos{int(math.Floor(x / 16)), int(math.Floor(z / 16))}
}

// Для целых координат блока то же делает сдвиг: `>>` у знаковых
// целых в Go арифметический, `-1 >> 4 == -1`.
func blockChunk(bx, bz int) ChunkPos { return ChunkPos{bx >> 4, bz >> 4} }

// Попасть в зону видимости — значит отстоять от центра не больше
// чем на r по каждой оси. Множества для этого не нужны.
func inView(center, c ChunkPos, r int) bool {
	return abs(c.X-center.X) <= r && abs(c.Z-center.Z) <= r
}

// Разность квадратов: чанки квадрата `to`, которых нет в `from`.
// `diff(old, new)` — что загрузить, `diff(new, old)` — что выгрузить.
// Ближние идут первыми: земля под ногами важнее горизонта.
func diff(from ChunkPos, fromR int, to ChunkPos, toR int) []ChunkPos {
	var out []ChunkPos
	for x := to.X - toR; x <= to.X+toR; x++ {
		for z := to.Z - toR; z <= to.Z+toR; z++ {
			if c := (ChunkPos{x, z}); !inView(from, c, fromR) {
				out = append(out, c)
			}
		}
	}
	slices.SortFunc(out, func(a, b ChunkPos) int {
		return cmp.Or(cmp.Compare(dist2(a, to), dist2(b, to)),
			cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z))
	})
	return out
}

type player struct {
	center ChunkPos
	r      int
}

// Мир держит обратный индекс «чанк → подписчики»: по нему за O(1)
// находят, кому слать событие из чанка, без перебора игроков.
type World struct {
	players map[string]*player
	subs    map[ChunkPos]map[string]bool
	dirty   map[ChunkPos]int // изменённые за тик блоки по чанкам
}

func (w *World) Join(name string, x, z float64, r int) {
	p := &player{center: chunkOf(x, z), r: r}
	w.players[name] = p
	// Пустой квадрат «из ниоткуда»: r = -1 не содержит ни одного чанка.
	w.apply(name, diff(p.center, -1, p.center, r), nil)
}

// Перемещение внутри чанка не стоит ничего. На границе —
// загрузка и выгрузка полосы, а не всего квадрата заново.
func (w *World) Move(name string, x, z float64) {
	p := w.players[name]
	c := chunkOf(x, z)
	if c == p.center {
		fmt.Printf("%s: %.1f,%.1f — тот же чанк %v, пакетов нет\n", name, x, z, c)
		return
	}
	load, unload := diff(p.center, p.r, c, p.r), diff(c, p.r, p.center, p.r)
	p.center = c
	w.apply(name, load, unload)
}

func (w *World) SetViewDistance(name string, r int) {
	p := w.players[name]
	load, unload := diff(p.center, p.r, p.center, r), diff(p.center, r, p.center, p.r)
	p.r = r
	w.apply(name, load, unload)
}

func (w *World) apply(name string, load, unload []ChunkPos) {
	for _, c := range unload {
		delete(w.subs[c], name)
		if len(w.subs[c]) == 0 {
			delete(w.subs, c) // пустые записи не копим
		}
	}
	for _, c := range load {
		if w.subs[c] == nil {
			w.subs[c] = map[string]bool{}
		}
		w.subs[c][name] = true
	}
	fmt.Printf("%s → чанк %v: загрузить %s, выгрузить %s\n",
		name, w.players[name].center, short(load), short(unload))
}

// Кому слать событие из чанка — его подписчикам, и только им.
func (w *World) Subscribers(c ChunkPos) []string {
	var out []string
	for name := range w.subs[c] {
		out = append(out, name)
	}
	slices.Sort(out) // порядок обхода map случаен
	return out
}

// Движение игрока видят подписчики его чанка, кроме него самого.
func (w *World) Viewers(name string) []string {
	return slices.DeleteFunc(w.Subscribers(w.players[name].center),
		func(n string) bool { return n == name })
}

// Блок изменён: событие не уходит сразу, а копится до конца тика.
func (w *World) SetBlock(bx, bz int) {
	c := blockChunk(bx, bz)
	w.dirty[c]++
	fmt.Printf("блок %d,%d в чанке %v: подписчики %v\n", bx, bz, c, w.Subscribers(c))
}

// Конец тика: по одному пакету «изменены блоки секции» на чанк.
// Отправка — в исходящие очереди соединений (урок 05), тик не ждёт.
func (w *World) EndTick() {
	chunks := make([]ChunkPos, 0, len(w.dirty))
	for c := range w.dirty {
		chunks = append(chunks, c)
	}
	slices.SortFunc(chunks, func(a, b ChunkPos) int {
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z))
	})
	for _, c := range chunks {
		subs := w.Subscribers(c)
		if len(subs) == 0 {
			fmt.Printf("конец тика: чанк %v никто не видит — пакета нет\n", c)
			continue
		}
		fmt.Printf("конец тика: чанк %v, блоков %d → 1 пакет для %v\n", c, w.dirty[c], subs)
	}
	clear(w.dirty)
}

func main() {
	w := &World{players: map[string]*player{},
		subs: map[ChunkPos]map[string]bool{}, dirty: map[ChunkPos]int{}}

	x := -0.5
	fmt.Println("x=-0.5: floor →", chunkOf(x, 0).X, "| int(x)/16 →", int(x)/16)

	// Дальность 2: квадрат 5×5 = 25 чанков.
	w.Join("alice", 8, 8, 2)
	w.Join("bob", 40, 8, 2)
	w.Move("alice", 15.9, 8)
	w.Move("alice", 16.1, 8)
	w.Move("alice", 33, 20) // по диагонали: 9 чанков туда и 9 обратно

	fmt.Println("движение bob увидят:", w.Viewers("bob"))
	w.SetBlock(20, 3)
	w.SetBlock(21, 3)
	w.SetBlock(-30, 3)
	w.EndTick()

	// Телепорт: квадраты не пересекаются — всё выгрузить, всё загрузить.
	w.Move("alice", 1000, -1000)
	fmt.Println("движение bob увидят:", w.Viewers("bob"))
	w.SetViewDistance("bob", 3)
	w.SetViewDistance("bob", 1)
	fmt.Println("подписок на чанки:", len(w.subs))
}

func abs(v int) int { return max(v, -v) }

func dist2(a, b ChunkPos) int { return (a.X-b.X)*(a.X-b.X) + (a.Z-b.Z)*(a.Z-b.Z) }

// Длинные списки печатаем числом.
func short(cs []ChunkPos) string {
	if len(cs) > 5 {
		return fmt.Sprintf("%d шт.", len(cs))
	}
	if len(cs) == 0 {
		return "—"
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = c.String()
	}
	return strings.Join(parts, " ")
}

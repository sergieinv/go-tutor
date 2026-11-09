package task

import "slices"

// Color — цвет объекта в трёхцветной маркировке.
type Color int

const (
	White Color = iota // не найден маркировкой
	Grey               // найден, но его указатели ещё не просмотрены
	Black              // найден и просмотрен
)

func (c Color) String() string {
	switch c {
	case White:
		return "белый"
	case Grey:
		return "серый"
	case Black:
		return "чёрный"
	}
	return "?"
}

// Nil — «пустой указатель»: слот ни на что не ссылается.
const Nil = -1

type object struct {
	slots []int
	color Color
	live  bool
}

// Heap — модель кучи со сборщиком мусора.
type Heap struct {
	objs    []object
	roots   map[int]bool
	marking bool
	grey    []int // очередь серых, FIFO
}

// NewHeap возвращает пустую кучу без корней; цикл GC не идёт.
func NewHeap() *Heap {
	return &Heap{roots: map[int]bool{}}
}

// Alloc создаёт объект с n слотами-указателями (все Nil) и возвращает
// его ID: 0, 1, 2… по порядку создания. Во время маркировки новый
// объект сразу чёрный, иначе — белый.
func (h *Heap) Alloc(n int) int {
	slots := make([]int, n)
	for i := range slots {
		slots[i] = Nil
	}
	c := White
	if h.marking {
		c = Black
	}
	h.objs = append(h.objs, object{slots: slots, color: c, live: true})
	return len(h.objs) - 1
}

// AddRoot и RemoveRoot меняют набор корней (переменные на стеке
// и глобальные). Записи в стек идут без барьера.
func (h *Heap) AddRoot(id int)    { h.roots[id] = true }
func (h *Heap) RemoveRoot(id int) { delete(h.roots, id) }

// Read возвращает значение слота.
func (h *Heap) Read(id, slot int) int { return h.objs[id].slots[slot] }

// Write записывает в слот объекта указатель ptr (или Nil). Во время
// маркировки работает гибридный барьер записи: сначала красится
// старое значение слота, затем новое.
func (h *Heap) Write(id, slot, ptr int) {
	if h.marking {
		h.shade(h.objs[id].slots[slot]) // Юаса: не потерять удаляемую ссылку
		h.shade(ptr)                    // Дейкстра: не спрятать объект в чёрном
	}
	h.objs[id].slots[slot] = ptr
}

// shade красит белый объект в серый и ставит его в очередь.
func (h *Heap) shade(id int) {
	if id == Nil || h.objs[id].color != White {
		return
	}
	h.objs[id].color = Grey
	h.grey = append(h.grey, id)
}

// StartGC начинает маркировку: корни красятся в серый
// по возрастанию ID.
func (h *Heap) StartGC() {
	h.marking = true
	roots := make([]int, 0, len(h.roots))
	for id := range h.roots {
		roots = append(roots, id)
	}
	slices.Sort(roots)
	for _, id := range roots {
		h.shade(id)
	}
}

// MarkStep берёт самый старый серый объект, красит в серый белые
// объекты из его слотов (по порядку слотов) и делает его чёрным.
// Возвращает false, если серых не осталось или маркировка не идёт.
func (h *Heap) MarkStep() bool {
	if !h.marking || len(h.grey) == 0 {
		return false
	}
	id := h.grey[0]
	h.grey = h.grey[1:]
	for _, p := range h.objs[id].slots {
		h.shade(p)
	}
	h.objs[id].color = Black
	return true
}

// Sweep доводит маркировку до конца, освобождает все белые объекты
// и возвращает их ID по возрастанию (пустой срез, если мусора нет).
// Выжившие снова становятся белыми, маркировка выключается.
// Без идущей маркировки Sweep ничего не делает и возвращает пустой срез.
func (h *Heap) Sweep() []int {
	freed := []int{}
	if !h.marking {
		return freed
	}
	for h.MarkStep() {
	}
	for id := range h.objs {
		o := &h.objs[id]
		if !o.live {
			continue
		}
		if o.color == White {
			o.live = false
			freed = append(freed, id)
			continue
		}
		o.color = White
	}
	h.marking = false
	return freed
}

// Color возвращает цвет объекта.
func (h *Heap) Color(id int) Color { return h.objs[id].color }

// Live сообщает, что объект ещё не освобождён.
func (h *Heap) Live(id int) bool { return h.objs[id].live }

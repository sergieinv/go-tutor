package task

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

// Heap — модель кучи со сборщиком мусора.
type Heap struct {
	// TODO: объекты, корни, фаза, очередь серых
}

// NewHeap возвращает пустую кучу без корней; цикл GC не идёт.
func NewHeap() *Heap {
	return &Heap{}
}

// Alloc создаёт объект с n слотами-указателями (все Nil) и возвращает
// его ID: 0, 1, 2… по порядку создания. Во время маркировки новый
// объект сразу чёрный, иначе — белый.
func (h *Heap) Alloc(n int) int {
	// TODO
	return 0
}

// AddRoot и RemoveRoot меняют набор корней (переменные на стеке
// и глобальные). Записи в стек идут без барьера.
func (h *Heap) AddRoot(id int)    {}
func (h *Heap) RemoveRoot(id int) {}

// Read возвращает значение слота.
func (h *Heap) Read(id, slot int) int {
	// TODO
	return Nil
}

// Write записывает в слот объекта указатель ptr (или Nil). Во время
// маркировки работает гибридный барьер записи: сначала красится
// старое значение слота, затем новое.
func (h *Heap) Write(id, slot, ptr int) {
	// TODO
}

// StartGC начинает маркировку: корни красятся в серый
// по возрастанию ID.
func (h *Heap) StartGC() {
	// TODO
}

// MarkStep берёт самый старый серый объект, красит в серый белые
// объекты из его слотов (по порядку слотов) и делает его чёрным.
// Возвращает false, если серых не осталось или маркировка не идёт.
func (h *Heap) MarkStep() bool {
	// TODO
	return false
}

// Sweep доводит маркировку до конца, освобождает все белые объекты
// и возвращает их ID по возрастанию (пустой срез, если мусора нет).
// Выжившие снова становятся белыми, маркировка выключается.
// Без идущей маркировки Sweep ничего не делает и возвращает пустой срез.
func (h *Heap) Sweep() []int {
	// TODO
	return nil
}

// Color возвращает цвет объекта.
func (h *Heap) Color(id int) Color {
	// TODO
	return White
}

// Live сообщает, что объект ещё не освобождён.
func (h *Heap) Live(id int) bool {
	// TODO
	return false
}

package task

// Snapshot — согласованная копия мира на тике Tick.
type Snapshot struct {
	Tick uint64
	// TODO: чанки снапшота
}

// Block возвращает блок снапшота в точке (x, z); 0 — воздух.
func (s *Snapshot) Block(x, z int) uint16 {
	// TODO
	return 0
}

// Chunks возвращает число чанков в снапшоте.
func (s *Snapshot) Chunks() int {
	// TODO
	return 0
}

// World — мир из чанков 16×16. Все методы вызывает одна горутина —
// горутина тика; функция save работает в своей горутине.
type World struct {
	// TODO
}

func NewWorld(save func(*Snapshot) error) *World {
	// TODO
	return &World{}
}

func (w *World) Tick() {
	// TODO
}

func (w *World) CurrentTick() uint64 {
	// TODO
	return 0
}

func (w *World) SetBlock(x, z int, b uint16) {
	// TODO
}

func (w *World) Block(x, z int) uint16 {
	// TODO
	return 0
}

// Copies возвращает, сколько раз чанк был скопирован перед записью.
func (w *World) Copies() int {
	// TODO
	return 0
}

// StartSave делает снапшот и запускает save в отдельной горутине.
func (w *World) StartSave() bool {
	// TODO
	return false
}

// Wait ждёт идущее сохранение и возвращает ошибку последнего
// завершённого.
func (w *World) Wait() error {
	// TODO
	return nil
}

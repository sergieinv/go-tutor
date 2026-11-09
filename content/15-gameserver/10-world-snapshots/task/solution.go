package task

import "maps"

type chunkPos struct{ X, Z int32 }

// chunk — 16×16 блоков. gen — поколение, в котором чанк создан
// или скопирован; чанк старшего поколения может читать снапшот.
type chunk struct {
	gen    uint64
	blocks [16 * 16]uint16
}

func posOf(x, z int) (chunkPos, int) {
	return chunkPos{int32(x >> 4), int32(z >> 4)}, (z&15)*16 + (x & 15)
}

// Snapshot — согласованная копия мира на тике Tick. Чанки снапшота
// никто не меняет: мир копирует чанк перед первой записью в него.
type Snapshot struct {
	Tick   uint64
	chunks map[chunkPos]*chunk
}

func (s *Snapshot) Block(x, z int) uint16 {
	pos, i := posOf(x, z)
	if c := s.chunks[pos]; c != nil {
		return c.blocks[i]
	}
	return 0
}

func (s *Snapshot) Chunks() int { return len(s.chunks) }

type World struct {
	save    func(*Snapshot) error
	tick    uint64
	gen     uint64
	chunks  map[chunkPos]*chunk
	copies  int
	busy    bool       // идёт сохранение; трогает только горутина тика
	done    chan error // горутина записи сообщает результат
	lastErr error
}

func NewWorld(save func(*Snapshot) error) *World {
	return &World{save: save, chunks: map[chunkPos]*chunk{}, done: make(chan error, 1)}
}

func (w *World) Tick()               { w.tick++ }
func (w *World) CurrentTick() uint64 { return w.tick }
func (w *World) Copies() int         { return w.copies }

func (w *World) SetBlock(x, z int, b uint16) {
	pos, i := posOf(x, z)
	c := w.chunks[pos]
	switch {
	case c == nil:
		c = &chunk{gen: w.gen}
		w.chunks[pos] = c
	case c.gen != w.gen:
		cp := *c
		cp.gen = w.gen
		c = &cp
		w.chunks[pos] = c
		w.copies++
	}
	c.blocks[i] = b
}

func (w *World) Block(x, z int) uint16 {
	pos, i := posOf(x, z)
	if c := w.chunks[pos]; c != nil {
		return c.blocks[i]
	}
	return 0
}

// poll забирает результат завершившегося сохранения, не блокируясь.
func (w *World) poll() {
	if !w.busy {
		return
	}
	select {
	case err := <-w.done:
		w.busy = false
		w.lastErr = err
	default:
	}
}

func (w *World) StartSave() bool {
	w.poll()
	if w.busy {
		return false
	}
	snap := &Snapshot{Tick: w.tick, chunks: maps.Clone(w.chunks)}
	w.gen++ // все чанки теперь общие со снапшотом
	w.busy = true
	go func() { w.done <- w.save(snap) }()
	return true
}

func (w *World) Wait() error {
	if w.busy {
		w.lastErr = <-w.done
		w.busy = false
	}
	return w.lastErr
}

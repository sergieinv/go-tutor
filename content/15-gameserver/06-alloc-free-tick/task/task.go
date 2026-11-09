package task

const IDEntityPositions = 0x20

type Entity struct {
	ID     int32
	X, Z   float64
	VX, VZ float64
}

// Viewer — игрок: видит сущности в радиусе Radius. Out — его кадры
// за последний тик.
type Viewer struct {
	X, Z   float64
	Radius float64
	Out    []byte
}

type World struct {
	Entities    []Entity
	Viewers     []*Viewer
	MaxPerFrame int // больше сущностей в одном кадре не класть; <= 0 — без лимита

	// TODO: свои буферы, которые живут между тиками
}

func (w *World) Tick() {
	// TODO: сдвинуть сущности, затем каждому игроку — кадры с видимыми
	// сущностями в v.Out; после прогрева — без выделений памяти
}

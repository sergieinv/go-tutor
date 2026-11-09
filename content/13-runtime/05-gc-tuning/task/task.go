package task

import (
	"errors"
	"math"
)

const (
	// MiB — мебибайт.
	MiB = 1 << 20
	// HeapMinimum — минимальная цель кучи при GOGC=100.
	HeapMinimum = 4 * MiB
	// NoLimit — значение GOMEMLIMIT по умолчанию: лимита нет.
	NoLimit = math.MaxInt64
)

// State — входы pacer'а после очередного цикла GC.
type State struct {
	HeapMarked  uint64 // живая куча: байты, найденные маркировкой
	StackScan   uint64 // байты стеков, просмотренные в прошлом цикле
	GlobalsScan uint64 // байты глобальных переменных с указателями
	GOGC        int    // процент; отрицательное значение — GOGC=off
	MemoryLimit uint64 // мягкий лимит памяти, NoLimit — лимита нет
	NonHeap     uint64 // память рантайма, не занятая объектами кучи
}

// HeapGoal возвращает цель кучи — размер, при котором должен
// закончиться следующий цикл GC.
func HeapGoal(s State) uint64 {
	// TODO: меньшая из целей по GOGC и по мягкому лимиту
	return 0
}

// ErrBadInput — неверное содержимое memory.max или процент запаса.
var ErrBadInput = errors.New("bad input")

// MaxContainerBytes — самый большой лимит контейнера, который
// принимает MemoryLimit (1 ТиБ).
const MaxContainerBytes = 1 << 40

// MemoryLimit считает GOMEMLIMIT по лимиту памяти контейнера.
func MemoryLimit(memoryMax string, reservePercent int) (int64, error) {
	// TODO
	return 0, nil
}

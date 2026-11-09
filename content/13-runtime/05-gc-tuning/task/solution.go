package task

import (
	"errors"
	"math"
	"strconv"
	"strings"
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
	return min(gogcGoal(s), limitGoal(s))
}

// gogcGoal — цель по GOGC (gcControllerState.commit в mgcpacer.go).
func gogcGoal(s State) uint64 {
	if s.GOGC < 0 {
		return math.MaxUint64 // GOGC=off: по проценту сборка не нужна никогда
	}
	gogc := uint64(s.GOGC)
	goal := s.HeapMarked + (s.HeapMarked+s.StackScan+s.GlobalsScan)*gogc/100
	return max(goal, HeapMinimum*gogc/100)
}

// limitGoal — цель по мягкому лимиту (memoryLimitHeapGoal в mgcpacer.go).
func limitGoal(s State) uint64 {
	if s.NonHeap >= s.MemoryLimit {
		return s.HeapMarked // лимит съеден без кучи: собирать непрерывно
	}
	goal := s.MemoryLimit - s.NonHeap
	headroom := max(goal/100*3, MiB) // запас на неточность pacer'а
	if goal < headroom || goal-headroom < headroom {
		goal = headroom
	} else {
		goal -= headroom
	}
	return max(goal, s.HeapMarked) // цель ниже живой кучи не имеет смысла
}

// ErrBadInput — неверное содержимое memory.max или процент запаса.
var ErrBadInput = errors.New("bad input")

// MemoryLimit считает GOMEMLIMIT по лимиту памяти контейнера.
func MemoryLimit(memoryMax string, reservePercent int) (int64, error) {
	if reservePercent < 0 || reservePercent >= 100 {
		return 0, ErrBadInput
	}
	v := strings.TrimSpace(memoryMax)
	if v == "max" {
		return NoLimit, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 || n > MaxContainerBytes {
		return 0, ErrBadInput
	}
	return n * int64(100-reservePercent) / 100, nil
}

// MaxContainerBytes — самый большой лимит контейнера, который
// принимает MemoryLimit (1 ТиБ).
const MaxContainerBytes = 1 << 40

package task

import (
	"errors"
	"math"
	"testing"
)

func TestHeapGoalGOGC(t *testing.T) {
	// 100 МиБ живой кучи, 2 МиБ стеков, 1 МиБ глобальных, GOGC=100:
	// 100 + (100+2+1)·100/100 = 203 МиБ.
	s := State{HeapMarked: 100 * MiB, StackScan: 2 * MiB, GlobalsScan: 1 * MiB, GOGC: 100, MemoryLimit: NoLimit}
	if got, want := HeapGoal(s), uint64(203*MiB); got != want {
		t.Errorf("HeapGoal = %d, want %d", got, want)
	}
}

func TestHeapGoalMinimum(t *testing.T) {
	// Крошечная куча: цель не меньше 4 МиБ·GOGC/100.
	s := State{HeapMarked: 100_000, GOGC: 100, MemoryLimit: NoLimit}
	if got := HeapGoal(s); got != HeapMinimum {
		t.Errorf("HeapGoal = %d, want %d", got, uint64(HeapMinimum))
	}
}

func TestHeapGoalMemoryLimit(t *testing.T) {
	// Лимит 200 МиБ, вне кучи 40 МиБ: 160 МиБ − 3% = 160·MiB − 160·MiB/100·3.
	s := State{HeapMarked: 100 * MiB, GOGC: 100, MemoryLimit: 200 * MiB, NonHeap: 40 * MiB}
	goal := uint64(160 * MiB)
	want := goal - goal/100*3
	if got := HeapGoal(s); got != want {
		t.Errorf("HeapGoal = %d, want %d", got, want)
	}
}

func TestMemoryLimit(t *testing.T) {
	got, err := MemoryLimit("536870912\n", 10) // 512 МиБ
	if err != nil || got != 483183820 {
		t.Errorf(`MemoryLimit("536870912\n", 10) = %d, %v; want 483183820, nil`, got, err)
	}
	if got, err := MemoryLimit("max\n", 10); err != nil || got != math.MaxInt64 {
		t.Errorf(`MemoryLimit("max\n", 10) = %d, %v; want NoLimit, nil`, got, err)
	}
	if _, err := MemoryLimit("lots", 10); !errors.Is(err, ErrBadInput) {
		t.Errorf(`MemoryLimit("lots", 10): err = %v, want ErrBadInput`, err)
	}
}

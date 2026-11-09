package task

import (
	"errors"
	"math"
	"testing"
)

func TestHiddenHeapGoal(t *testing.T) {
	tests := []struct {
		name string
		s    State
		want uint64
	}{
		{
			// Стеки и глобальные входят в базу: 10 + (10+6+4)·50/100 = 20 МиБ.
			"стеки и глобальные", State{HeapMarked: 10 * MiB, StackScan: 6 * MiB, GlobalsScan: 4 * MiB, GOGC: 50, MemoryLimit: NoLimit},
			20 * MiB,
		},
		{
			// Минимум тоже масштабируется: max(999 + 999·33/100, 4 МиБ·33/100).
			"минимум при GOGC=33", State{HeapMarked: 999, GOGC: 33, MemoryLimit: NoLimit},
			4 * MiB * 33 / 100,
		},
		{
			// Сначала умножаем, потом делим: (x·33)/100 и x/100·33 здесь различаются.
			"порядок умножения и деления", State{HeapMarked: 100*MiB + 99, GOGC: 33, MemoryLimit: NoLimit},
			100*MiB + 99 + (100*MiB+99)*33/100,
		},
		{
			// Минимум масштабируется GOGC: 4 МиБ·200/100 = 8 МиБ.
			"минимум при GOGC=200", State{HeapMarked: 1 * MiB, GOGC: 200, MemoryLimit: NoLimit},
			8 * MiB,
		},
		{
			// GOGC=0: минимум 0, цель — сама живая куча.
			"GOGC=0", State{HeapMarked: 5 * MiB, StackScan: MiB, GOGC: 0, MemoryLimit: NoLimit},
			5 * MiB,
		},
		{
			// Обе цели «бесконечны», меньшая — цель по лимиту NoLimit.
			"GOGC=off без лимита", State{HeapMarked: 5 * MiB, GOGC: -1, MemoryLimit: NoLimit},
			math.MaxInt64 - math.MaxInt64/100*3,
		},
		{
			// GOGC=off + лимит: работает только лимит. 1000 − 30 = 970 МиБ.
			"GOGC=off с лимитом", State{HeapMarked: 300 * MiB, GOGC: -1, MemoryLimit: 1024 * MiB, NonHeap: 24 * MiB},
			1000*MiB - 1000*MiB/100*3,
		},
		{
			// Лимит далеко — побеждает GOGC.
			"лимит не мешает", State{HeapMarked: 10 * MiB, GOGC: 100, MemoryLimit: 1 << 30, NonHeap: 10 * MiB},
			20 * MiB,
		},
		{
			// 3% от 20 МиБ меньше 1 МиБ — запас 1 МиБ: 20 − 1 = 19 МиБ.
			"минимальный запас 1 МиБ", State{HeapMarked: 2 * MiB, GOGC: 1000, MemoryLimit: 30 * MiB, NonHeap: 10 * MiB},
			19 * MiB,
		},
		{
			// goal−headroom < headroom: цель = запас. 1,5 МиБ → 1 МиБ.
			"маленький остаток", State{HeapMarked: 100, GOGC: 100, MemoryLimit: 3 * MiB, NonHeap: 3 * MiB / 2},
			MiB,
		},
		{
			// Цель по лимиту ниже живой кучи — поднимается до неё.
			"не ниже живой кучи", State{HeapMarked: 90 * MiB, GOGC: 100, MemoryLimit: 100 * MiB, NonHeap: 20 * MiB},
			90 * MiB,
		},
		{
			// Вне кучи уже больше лимита — цель = живая куча, GC без остановки.
			"лимит превышен без кучи", State{HeapMarked: 7 * MiB, GOGC: 100, MemoryLimit: 64 * MiB, NonHeap: 64 * MiB},
			7 * MiB,
		},
		{
			"лимит 0", State{HeapMarked: 7 * MiB, GOGC: -1, MemoryLimit: 0},
			7 * MiB,
		},
	}
	for _, tt := range tests {
		if got := HeapGoal(tt.s); got != tt.want {
			t.Errorf("%s: HeapGoal(%+v) = %d, want %d", tt.name, tt.s, got, tt.want)
		}
	}
}

func TestHiddenMemoryLimit(t *testing.T) {
	tests := []struct {
		in      string
		reserve int
		want    int64
	}{
		{"1073741824", 10, 966367641},
		{"  268435456  \n", 0, 268435456},
		{"1000", 25, 750},
		{"999", 50, 499},
		{"1", 99, 0},
		{"max", 0, math.MaxInt64},
		{" max\n", 99, math.MaxInt64},
		{"1099511627776", 10, 989560464998},
	}
	for _, tt := range tests {
		got, err := MemoryLimit(tt.in, tt.reserve)
		if err != nil || got != tt.want {
			t.Errorf("MemoryLimit(%q, %d) = %d, %v; want %d, nil", tt.in, tt.reserve, got, err, tt.want)
		}
	}
}

func TestHiddenMemoryLimitErrors(t *testing.T) {
	tests := []struct {
		in      string
		reserve int
	}{
		{"", 10},
		{"\n", 10},
		{"0", 10},
		{"-512", 10},
		{"12.5", 10},
		{"512M", 10},
		{"MAX", 10},
		{"1099511627777", 10},
		{"99999999999999999999", 10},
		{"1024", -1},
		{"1024", 100},
		{"max", 100},
		{"max", -5},
	}
	for _, tt := range tests {
		got, err := MemoryLimit(tt.in, tt.reserve)
		if !errors.Is(err, ErrBadInput) || got != 0 {
			t.Errorf("MemoryLimit(%q, %d) = %d, %v; want 0, ErrBadInput", tt.in, tt.reserve, got, err)
		}
	}
}

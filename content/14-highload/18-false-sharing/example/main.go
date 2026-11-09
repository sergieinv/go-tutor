// Процессор обменивается с памятью не байтами, а кеш-линиями
// (64 байта на x86-64, 128 на Apple M). Две горутины, которые
// пишут каждая в свою переменную, всё равно мешают друг другу,
// если переменные лежат в одной линии, — это false sharing.
// Пример с тестами: размеры и корректность проверяют тесты,
// а цену общей линии покажет кнопка «Бенчмарки».

package main

import (
	"sync/atomic"
	"unsafe"
)

func main() {}

// Размер, до которого выравниваем. 128 покрывает и 64-байтные
// линии x86-64 (вместе с предвыборкой соседней линии), и
// 128-байтные линии Apple M — так же делает рантайм Go для
// arm64 (`internal/cpu.CacheLinePadSize`) и `sync.Pool`.
const CacheLine = 128

// Слотов больше, чем горутин в бенчмарке: у каждой — свой.
const Slots = 16

// Плотный массив: 16 счётчиков по 8 байт помещаются в одну-две
// линии. У каждой горутины свой счётчик — гонки нет, а линия
// одна на всех.
type Packed [Slots]atomic.Int64

// Счётчик с хвостом-заполнителем занимает линию целиком.
// Поле `_` без имени нельзя прочитать — это только отступ.
type paddedSlot struct {
	n atomic.Int64
	_ [CacheLine - unsafe.Sizeof(atomic.Int64{})]byte
}

// Соседние счётчики теперь на расстоянии `CacheLine` байт и не
// могут попасть в одну линию, где бы ни начинался массив.
type Padded [Slots]paddedSlot

func (p *Packed) Inc(i int) { p[i%Slots].Add(1) }
func (p *Padded) Inc(i int) { p[i%Slots].n.Add(1) }

func (p *Packed) Sum() (s int64) {
	for i := range p {
		s += p[i].Load()
	}
	return s
}

func (p *Padded) Sum() (s int64) {
	for i := range p {
		s += p[i].n.Load()
	}
	return s
}

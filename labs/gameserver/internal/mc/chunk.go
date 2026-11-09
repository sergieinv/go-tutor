package mc

import (
	"math/bits"

	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/world"
)

// Формат чанка (Chunk Data and Update Light, протокол 767):
//
//	Int x, Int z
//	NBT heightmaps {MOTION_BLOCKING: long[37]}
//	VarInt size, data: 24 секции
//	    Short число непустых блоков
//	    paletted container блоков (4096 значений)
//	    paletted container биомов (64 значения)
//	VarInt число block entities (0)
//	свет: 4 BitSet-маски и массивы по 2048 байт (полубайт на блок)
//
// Плоский мир почти весь одинаковый, поэтому всё, что не зависит от
// правок, кодируется один раз при старте: базовые секции, карта высот и
// «хвост» со светом. Готовые пакеты чанков кешируются по (позиция, версия).

const (
	lightSections = world.SectionCount + 2 // секция под миром и над ним
	globalBits    = 15                     // ceil(log2(26684)) — прямая палитра 1.21.1
)

// chunkStatic — неизменяемые части пакета чанка.
type chunkStatic struct {
	heightmaps []byte
	sections   [world.SectionCount][]byte // базовые секции генератора
	tail       []byte                     // block entities + свет
}

func newChunkStatic(enc *sectionEncoder) *chunkStatic {
	s := &chunkStatic{}

	// Карта высот: высота над MinY верхнего твёрдого блока + 1, 9 бит на
	// значение (384 → нужно 9 бит), 7 значений в long — 37 long'ов.
	const hBits, perLong = 9, 64 / 9
	h := int64(world.GroundY - world.MinY + 1)
	longs := make([]int64, (256+perLong-1)/perLong)
	for i := 0; i < 256; i++ {
		longs[i/perLong] |= h << (uint(i%perLong) * hBits)
	}
	s.heightmaps = mcproto.AppendHeightmaps(nil, longs)

	var blocks [4096]world.State
	for sec := 0; sec < world.SectionCount; sec++ {
		baseY := int32(world.MinY + sec*16)
		for i := range blocks {
			x, y, z := int32(i&15), int32(i>>8), int32((i>>4)&15)
			blocks[i] = world.BaseBlock(x, baseY+y, z)
		}
		s.sections[sec] = enc.encode(nil, &blocks)
	}

	// Свет: небесный — 15 во всех 26 секциях (плоский мир под открытым
	// небом, клиент рисует всё освещённым), блочного света нет — его
	// секции помечены «пустыми». Без света клиент показал бы чёрный мир.
	t := mcproto.AppendVarInt(nil, 0) // block entities
	all := int64(1)<<lightSections - 1
	t = appendBitSet(t, all) // sky light mask
	t = appendBitSet(t, 0)   // block light mask
	t = appendBitSet(t, 0)   // empty sky light mask
	t = appendBitSet(t, all) // empty block light mask
	t = mcproto.AppendVarInt(t, lightSections)
	full := make([]byte, 2048)
	for i := range full {
		full[i] = 0xFF
	}
	for i := 0; i < lightSections; i++ {
		t = mcproto.AppendVarInt(t, 2048)
		t = append(t, full...)
	}
	t = mcproto.AppendVarInt(t, 0) // block light arrays
	s.tail = t
	return s
}

func appendBitSet(dst []byte, v int64) []byte {
	if v == 0 {
		return mcproto.AppendVarInt(dst, 0)
	}
	dst = mcproto.AppendVarInt(dst, 1)
	return mcproto.AppendLong(dst, v)
}

// sectionEncoder кодирует секцию 16×16×16 в paletted container.
// Переиспользует таблицу индексов палитры: без аллокаций на секцию.
type sectionEncoder struct {
	index   []int16 // state → индекс в палитре (-1 — нет)
	palette []world.State
}

func newSectionEncoder() *sectionEncoder {
	e := &sectionEncoder{index: make([]int16, world.StateCount()), palette: make([]world.State, 0, 256)}
	for i := range e.index {
		e.index[i] = -1
	}
	return e
}

// encode дописывает секцию: число непустых блоков, блоки, биомы.
func (e *sectionEncoder) encode(dst []byte, blocks *[4096]world.State) []byte {
	nonAir := 0
	e.palette = e.palette[:0]
	for _, s := range blocks {
		if !world.IsAir(s) {
			nonAir++
		}
		if e.index[s] < 0 && len(e.palette) <= 256 {
			e.index[s] = int16(len(e.palette))
			e.palette = append(e.palette, s)
		}
	}
	dst = mcproto.AppendShort(dst, int16(nonAir))

	switch n := len(e.palette); {
	case n == 1:
		// Одно значение на всю секцию: 0 бит, значение, пустой массив.
		dst = append(dst, 0)
		dst = mcproto.AppendVarInt(dst, int32(e.palette[0]))
		dst = mcproto.AppendVarInt(dst, 0)
	case n <= 256:
		// Своя палитра: 4–8 бит на блок, в данных — индексы палитры.
		b := max(4, bits.Len(uint(n-1)))
		dst = append(dst, byte(b))
		dst = mcproto.AppendVarInt(dst, int32(n))
		for _, s := range e.palette {
			dst = mcproto.AppendVarInt(dst, int32(s))
		}
		dst = e.pack(dst, blocks, b, true)
	default:
		// Прямая палитра: id состояний как есть, 15 бит.
		dst = append(dst, globalBits)
		dst = e.pack(dst, blocks, globalBits, false)
	}
	for _, s := range e.palette {
		e.index[s] = -1
	}

	// Биомы: одно значение (plains, id 0 в нашем реестре).
	dst = append(dst, 0)
	dst = mcproto.AppendVarInt(dst, 0)
	return mcproto.AppendVarInt(dst, 0)
}

// pack упаковывает значения в long'и: с 1.16 значение не пересекает
// границу long'а, в каждом long'е 64/bits значений, младшие — первыми.
func (e *sectionEncoder) pack(dst []byte, blocks *[4096]world.State, b int, indirect bool) []byte {
	perLong := 64 / b
	n := (4096 + perLong - 1) / perLong
	dst = mcproto.AppendVarInt(dst, int32(n))
	for l := 0; l < n; l++ {
		var v uint64
		for j := 0; j < perLong; j++ {
			i := l*perLong + j
			if i >= 4096 {
				break
			}
			val := uint64(blocks[i])
			if indirect {
				val = uint64(e.index[blocks[i]])
			}
			v |= val << (uint(j) * uint(b))
		}
		dst = mcproto.AppendLong(dst, int64(v))
	}
	return dst
}

// appendChunkBody дописывает тело пакета чанка (после id).
func (enc *Encoder) appendChunkBody(dst []byte, pos world.ChunkPos, col *world.Column) []byte {
	dst = mcproto.AppendInt(dst, pos.X)
	dst = mcproto.AppendInt(dst, pos.Z)
	dst = append(dst, enc.static.heightmaps...)

	// Секции с правками собираем из генератора и правок.
	data := enc.sectionBuf[:0]
	if col != nil {
		for s := 0; s < world.SectionCount; s++ {
			if col.SectionEdited(s) {
				baseY := int32(world.MinY + s*16)
				blk := &enc.scratch[s]
				for i := range blk {
					x, y, z := int32(i&15), int32(i>>8), int32((i>>4)&15)
					blk[i] = world.BaseBlock(pos.X*16+x, baseY+y, pos.Z*16+z)
				}
			}
		}
		col.Edits(func(i int32, st world.State) {
			x, y, z := world.LocalCoords(i)
			s := (y - world.MinY) >> 4
			enc.scratch[s][((y-world.MinY)&15)<<8|z<<4|x] = st
		})
	}
	for s := 0; s < world.SectionCount; s++ {
		if col.SectionEdited(s) {
			data = enc.sections.encode(data, &enc.scratch[s])
		} else {
			data = append(data, enc.static.sections[s]...)
		}
	}
	enc.sectionBuf = data
	dst = mcproto.AppendVarInt(dst, int32(len(data)))
	dst = append(dst, data...)
	return append(dst, enc.static.tail...)
}

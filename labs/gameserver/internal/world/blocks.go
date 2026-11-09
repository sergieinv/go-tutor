package world

import (
	"fmt"

	"github.com/Tnze/go-mc/level/block"
)

// State — id состояния блока в глобальной палитре Minecraft 1.21.1
// (0 — воздух). Таблицу состояний берём из go-mc (level/block): писать её
// руками бессмысленно, а данные Mojang в репозитории не храним.
type State uint16

// Геометрия мира: как у ванильного верхнего мира 1.18+.
const (
	MinY          = -64
	Height        = 384
	MaxY          = MinY + Height - 1 // 319
	SectionCount  = Height / 16       // 24 секции по 16 блоков
	ChunkSize     = 16
	GroundY       = -61         // верхний слой земли
	SpawnY        = GroundY + 1 // ноги игрока стоят на земле
	blocksPerCol  = ChunkSize * ChunkSize * Height
	blocksPerSect = 16 * 16 * 16
)

// Блоки плоского мира. Значения — из таблицы go-mc; тест сверяет их с
// известными id 1.21.1 (air 0, stone 1, grass_block 9, dirt 10, bedrock 79).
var (
	Air        = mustState(block.Air{})
	Stone      = mustState(block.Stone{})
	GrassBlock = mustState(block.GrassBlock{Snowy: false})
	Dirt       = mustState(block.Dirt{})
	Bedrock    = mustState(block.Bedrock{})
)

func mustState(b block.Block) State {
	id, ok := block.ToStateID[b]
	if !ok {
		panic(fmt.Sprintf("world: unknown block state %s", b.ID()))
	}
	return State(id)
}

// StateCount — число состояний в глобальной палитре (26684 в 1.21.1).
func StateCount() int { return len(block.StateList) }

// IsAir — воздух во всех трёх вариантах (air, cave_air, void_air).
func IsAir(s State) bool { return block.IsAir(block.StateID(s)) }

// StateName — имя блока состояния ("minecraft:stone").
func StateName(s State) string {
	if int(s) >= len(block.StateList) {
		return "minecraft:air"
	}
	return block.StateList[s].ID()
}

// DefaultState — состояние блока по имени, если у блока есть состояние
// с нулевыми свойствами (у простых блоков — единственное). ok=false для
// неизвестных имён и блоков без такого состояния.
func DefaultState(name string) (State, bool) {
	b, ok := block.FromID[name]
	if !ok {
		return 0, false
	}
	id, ok := block.ToStateID[b]
	if !ok {
		return 0, false
	}
	return State(id), true
}

// BaseBlock — генератор плоского мира: бедрок, два слоя земли и трава.
// Границы чанков выложены камнем — их видно и в клиенте Minecraft, и на
// карте браузерного клиента (уроки про чанки показывают это вживую).
func BaseBlock(x, y, z int32) State {
	switch {
	case y == MinY:
		return Bedrock
	case y > MinY && y < GroundY:
		return Dirt
	case y == GroundY:
		if x&15 == 0 || z&15 == 0 {
			return Stone
		}
		return GrassBlock
	default:
		return Air
	}
}

// Column — изменения одного чанка относительно генератора. Хранятся
// только отличия: нетронутый чанк не занимает памяти, а снапшот мира
// пишет только правки.
type Column struct {
	Pos ChunkPos
	// Version растёт при каждой правке — по ней кодировщики протоколов
	// понимают, что закешированный пакет чанка устарел.
	Version uint32
	edits   map[int32]State
	// sectionEdits — число правок в каждой секции: кодировщик берёт
	// заранее закодированную базовую секцию, если в ней правок нет.
	sectionEdits [SectionCount]uint16
}

// localIndex — индекс блока внутри колонки: y, затем z, затем x — тот же
// порядок, что в секциях протокола.
func localIndex(x, y, z int32) int32 {
	return (y-MinY)<<8 | (z&15)<<4 | x&15
}

// LocalCoords — обратное к localIndex: локальные x, z и мировой y.
func LocalCoords(i int32) (x, y, z int32) {
	return i & 15, i>>8 + MinY, (i >> 4) & 15
}

// SectionEdited — есть ли правки в секции s.
func (c *Column) SectionEdited(s int) bool { return c != nil && c.sectionEdits[s] > 0 }

// Edits вызывает f для каждой правки колонки (локальный индекс, состояние).
func (c *Column) Edits(f func(i int32, s State)) {
	if c == nil {
		return
	}
	for i, s := range c.edits {
		f(i, s)
	}
}

// Chunks — хранилище правок мира.
type Chunks struct {
	cols  map[ChunkPos]*Column
	edits int // всего правок во всех колонках
}

// Edits — сколько всего блоков отличается от генератора.
func (c *Chunks) Edits() int { return c.edits }

// AddsEdit — увеличит ли Set(x, y, z, s) число правок (нужно для предела:
// вернуть блок как было можно всегда).
func (c *Chunks) AddsEdit(x, y, z int32, s State) bool {
	if s == BaseBlock(x, y, z) {
		return false
	}
	if col := c.cols[ChunkPos{x >> 4, z >> 4}]; col != nil {
		if _, had := col.edits[localIndex(x, y, z)]; had {
			return false
		}
	}
	return true
}

func newChunks() *Chunks { return &Chunks{cols: make(map[ChunkPos]*Column)} }

// Column — колонка с правками или nil, если чанк не меняли.
func (c *Chunks) Column(p ChunkPos) *Column { return c.cols[p] }

// Block — состояние блока с учётом правок.
func (c *Chunks) Block(x, y, z int32) State {
	if y < MinY || y > MaxY {
		return Air
	}
	if col := c.cols[ChunkPos{x >> 4, z >> 4}]; col != nil {
		if s, ok := col.edits[localIndex(x, y, z)]; ok {
			return s
		}
	}
	return BaseBlock(x, y, z)
}

// Set меняет блок; false — если состояние уже такое.
func (c *Chunks) Set(x, y, z int32, s State) bool {
	if c.Block(x, y, z) == s {
		return false
	}
	p := ChunkPos{x >> 4, z >> 4}
	col := c.cols[p]
	if col == nil {
		col = &Column{Pos: p, edits: make(map[int32]State)}
		c.cols[p] = col
	}
	i := localIndex(x, y, z)
	sect := (y - MinY) >> 4
	_, had := col.edits[i]
	if s == BaseBlock(x, y, z) { // вернули как было — правка не нужна
		if had {
			delete(col.edits, i)
			col.sectionEdits[sect]--
			c.edits--
		}
	} else {
		if !had {
			col.sectionEdits[sect]++
			c.edits++
		}
		col.edits[i] = s
	}
	col.Version++
	return true
}

// TopBlock — самый верхний непрозрачный для взгляда сверху (не воздух)
// блок колонки (x, z): его рисует карта браузерного клиента.
func TopBlock(col *Column, x, z int32) (int32, State) {
	if col != nil && len(col.edits) > 0 {
		for y := int32(MaxY); y >= MinY; y-- {
			s, ok := col.edits[localIndex(x, y, z)]
			if !ok {
				s = BaseBlock(x, y, z)
			}
			if !IsAir(s) {
				return y, s
			}
		}
		return MinY, Air
	}
	return GroundY, BaseBlock(x, GroundY, z)
}

// edits возвращает все правки (для снапшота).
func (c *Chunks) appendEdits(dst []BlockEdit) []BlockEdit {
	for _, col := range c.cols {
		for i, s := range col.edits {
			lx, y, lz := LocalCoords(i)
			dst = append(dst, BlockEdit{
				X: col.Pos.X*16 + lx, Y: y, Z: col.Pos.Z*16 + lz, State: s,
			})
		}
	}
	return dst
}

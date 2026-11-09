package gateway

import (
	"math"
	"strconv"
	"unicode/utf8"

	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/world"
)

// Encoder — world.Protocol для браузерного клиента: каждое WebSocket-
// сообщение — JSON-массив событий одного тика, последним всегда идёт
// {"t":"tick",...}. JSON пишется вручную, в стиле append: encoding/json с
// рефлексией аллоцировал бы на каждое событие, а события кодируются в тике.
//
// Один экземпляр на всех браузерных клиентов, только из горутины тика.
type Encoder struct {
	cache map[world.ChunkPos]cachedChunk
}

type cachedChunk struct {
	version uint32
	edited  bool
	json    []byte
}

// NewEncoder создаёт кодировщик.
func NewEncoder() *Encoder { return &Encoder{cache: make(map[world.ChunkPos]cachedChunk)} }

func (e *Encoder) Begin(dst []byte) []byte { return append(dst, '[') }

// End дописывает статистику тика и закрывает массив.
func (e *Encoder) End(dst []byte, st *world.BatchStats) []byte {
	bytes := len(dst) - 1
	dst = append(dst, `{"t":"tick","n":`...)
	dst = strconv.AppendUint(dst, st.Tick, 10)
	dst = append(dst, `,"mspt":`...)
	dst = appendNum(dst, st.MSPT)
	dst = append(dst, `,"tps":`...)
	dst = appendNum(dst, st.TPS)
	dst = append(dst, `,"msgs":`...)
	dst = strconv.AppendInt(dst, int64(st.Events), 10)
	dst = append(dst, `,"bytes":`...)
	dst = strconv.AppendInt(dst, int64(bytes), 10)
	return append(dst, "}]"...)
}

// Append кодирует событие; каждое заканчивается запятой (массив
// закрывает End).
func (e *Encoder) Append(dst []byte, ev *world.Event) []byte {
	p := ev.Player
	switch ev.Kind {
	case world.EvWelcome:
		wl := ev.Welcome
		dst = append(dst, `{"t":"welcome","id":`...)
		dst = strconv.AppendInt(dst, int64(p.ID), 10)
		dst = append(dst, `,"name":`...)
		dst = appendString(dst, p.Name)
		dst = append(dst, `,"uuid":"`...)
		dst = append(dst, mcproto.FormatUUID(p.UUID)...)
		dst = append(dst, '"')
		dst = appendPos(dst, p.Pos, p.Yaw, p.Pitch)
		dst = append(dst, `,"tp":`...)
		dst = strconv.AppendInt(dst, int64(ev.TeleportID), 10)
		dst = append(dst, `,"viewDistance":`...)
		dst = strconv.AppendInt(dst, int64(wl.ViewDistance), 10)
		dst = append(dst, `,"tickRate":`...)
		dst = strconv.AppendInt(dst, int64(wl.TickRate), 10)
		dst = append(dst, `,"maxPlayers":`...)
		dst = strconv.AppendInt(dst, int64(wl.MaxPlayers), 10)
		dst = append(dst, `,"maxChat":`...)
		dst = strconv.AppendInt(dst, int64(wl.MaxChat), 10)
		dst = append(dst, `,"world":{"chunkSize":16,"minY":`...)
		dst = strconv.AppendInt(dst, world.MinY, 10)
		dst = append(dst, `,"height":`...)
		dst = strconv.AppendInt(dst, world.Height, 10)
		dst = append(dst, `,"groundY":`...)
		dst = strconv.AppendInt(dst, world.GroundY, 10)
		dst = append(dst, `,"borderRadius":`...)
		dst = appendNum(dst, wl.BorderRadius)
		dst = append(dst, `,"spawn":{"x":`...)
		dst = appendNum(dst, wl.Spawn.X)
		dst = append(dst, `,"y":`...)
		dst = appendNum(dst, wl.Spawn.Y)
		dst = append(dst, `,"z":`...)
		dst = appendNum(dst, wl.Spawn.Z)
		dst = append(dst, `}},"placeable":[`...)
		for i, n := range wl.Placeable {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, n)
		}
		dst = append(dst, "]},"...)

	case world.EvRosterAdd:
		dst = append(dst, `{"t":"join","id":`...)
		dst = strconv.AppendInt(dst, int64(p.ID), 10)
		dst = append(dst, `,"name":`...)
		dst = appendString(dst, p.Name)
		dst = append(dst, `,"uuid":"`...)
		dst = append(dst, mcproto.FormatUUID(p.UUID)...)
		dst = append(dst, `","kind":"`...)
		dst = append(dst, p.Kind.String()...)
		dst = append(dst, `"},`...)

	case world.EvRosterRemove:
		dst = append(dst, `{"t":"leave","id":`...)
		dst = strconv.AppendInt(dst, int64(p.ID), 10)
		dst = append(dst, `,"name":`...)
		dst = appendString(dst, p.Name)
		dst = append(dst, "},"...)

	case world.EvSpawn:
		dst = append(dst, `{"t":"spawn","id":`...)
		dst = strconv.AppendInt(dst, int64(p.ID), 10)
		dst = append(dst, `,"name":`...)
		dst = appendString(dst, p.Name)
		dst = append(dst, `,"kind":"`...)
		dst = append(dst, p.Kind.String()...)
		dst = append(dst, '"')
		dst = appendPos(dst, p.Pos, p.Yaw, p.Pitch)
		dst = append(dst, "},"...)

	case world.EvDespawn:
		dst = append(dst, `{"t":"despawn","id":`...)
		dst = strconv.AppendInt(dst, int64(p.ID), 10)
		dst = append(dst, "},"...)

	case world.EvMove:
		dst = append(dst, `{"t":"move","id":`...)
		dst = strconv.AppendInt(dst, int64(p.ID), 10)
		dst = appendPos(dst, ev.To, ev.Yaw, ev.Pitch)
		dst = append(dst, "},"...)

	case world.EvChat:
		dst = append(dst, `{"t":"chat","id":`...)
		dst = strconv.AppendInt(dst, int64(p.ID), 10)
		dst = append(dst, `,"name":`...)
		dst = appendString(dst, p.Name)
		dst = append(dst, `,"text":`...)
		dst = appendString(dst, ev.Text)
		dst = append(dst, "},"...)

	case world.EvSystem:
		dst = append(dst, `{"t":"system","text":`...)
		dst = appendString(dst, ev.Text)
		dst = append(dst, "},"...)

	case world.EvChunk:
		dst = append(dst, e.chunk(ev.Chunk, ev.Column)...)

	case world.EvUnchunk:
		dst = append(dst, `{"t":"unchunk","x":`...)
		dst = strconv.AppendInt(dst, int64(ev.Chunk.X), 10)
		dst = append(dst, `,"z":`...)
		dst = strconv.AppendInt(dst, int64(ev.Chunk.Z), 10)
		dst = append(dst, "},"...)

	case world.EvCenter:
		dst = append(dst, `{"t":"center","x":`...)
		dst = strconv.AppendInt(dst, int64(ev.Chunk.X), 10)
		dst = append(dst, `,"z":`...)
		dst = strconv.AppendInt(dst, int64(ev.Chunk.Z), 10)
		dst = append(dst, "},"...)

	case world.EvBlock:
		dst = append(dst, `{"t":"block","x":`...)
		dst = strconv.AppendInt(dst, int64(ev.Block.X), 10)
		dst = append(dst, `,"y":`...)
		dst = strconv.AppendInt(dst, int64(ev.Block.Y), 10)
		dst = append(dst, `,"z":`...)
		dst = strconv.AppendInt(dst, int64(ev.Block.Z), 10)
		dst = append(dst, `,"block":`...)
		dst = appendString(dst, world.StateName(ev.State))
		dst = append(dst, `,"topY":`...)
		dst = strconv.AppendInt(dst, int64(ev.TopY), 10)
		dst = append(dst, `,"top":`...)
		dst = appendString(dst, world.StateName(ev.TopState))
		dst = append(dst, "},"...)

	case world.EvTeleport:
		dst = append(dst, `{"t":"teleport","tp":`...)
		dst = strconv.AppendInt(dst, int64(ev.TeleportID), 10)
		dst = appendPos(dst, ev.To, ev.Yaw, ev.Pitch)
		dst = append(dst, `,"reason":`...)
		dst = appendString(dst, ev.Reason)
		dst = append(dst, "},"...)

	case world.EvKick:
		dst = appendKick(dst, ev.Text)
		dst = append(dst, ',')
	}
	// EvChunkBatchBegin/End и EvBlockAck браузеру не нужны.
	return dst
}

func appendKick(dst []byte, reason string) []byte {
	dst = append(dst, `{"t":"kick","reason":`...)
	dst = appendString(dst, reason)
	return append(dst, '}')
}

// kickMessage — отдельное сообщение [{"t":"kick",...}] вне тика (отказ до
// входа, нарушение протокола). Чистая функция: безопасна из любой горутины.
func kickMessage(reason string) []byte {
	b := []byte{'['}
	b = appendKick(b, reason)
	return append(b, ']')
}

// chunk — JSON чанка для карты сверху: палитра имён блоков и для каждой
// из 256 колонок (индекс z*16+x) — индекс верхнего блока в палитре и его y.
func (e *Encoder) chunk(pos world.ChunkPos, col *world.Column) []byte {
	var version uint32
	if col != nil {
		version = col.Version
	}
	if c, ok := e.cache[pos]; ok && c.version == version && c.edited == (col != nil) {
		return c.json
	}
	var tops [256]world.State
	var ys [256]int32
	palette := make([]world.State, 0, 8)
	idx := make(map[world.State]int, 8)
	for z := int32(0); z < 16; z++ {
		for x := int32(0); x < 16; x++ {
			y, s := world.TopBlock(col, pos.X*16+x, pos.Z*16+z)
			tops[z*16+x], ys[z*16+x] = s, y
			if _, ok := idx[s]; !ok {
				idx[s] = len(palette)
				palette = append(palette, s)
			}
		}
	}
	b := make([]byte, 0, 2048)
	b = append(b, `{"t":"chunk","x":`...)
	b = strconv.AppendInt(b, int64(pos.X), 10)
	b = append(b, `,"z":`...)
	b = strconv.AppendInt(b, int64(pos.Z), 10)
	b = append(b, `,"palette":[`...)
	for i, s := range palette {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendString(b, world.StateName(s))
	}
	b = append(b, `],"top":[`...)
	for i, s := range tops {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendInt(b, int64(idx[s]), 10)
	}
	b = append(b, `],"height":[`...)
	for i, y := range ys {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendInt(b, int64(y), 10)
	}
	b = append(b, "]},"...)
	if len(e.cache) > 16384 {
		clear(e.cache)
	}
	e.cache[pos] = cachedChunk{version: version, edited: col != nil, json: b}
	return b
}

func appendPos(dst []byte, p world.Vec3, yaw, pitch float32) []byte {
	dst = append(dst, `,"x":`...)
	dst = appendNum(dst, p.X)
	dst = append(dst, `,"y":`...)
	dst = appendNum(dst, p.Y)
	dst = append(dst, `,"z":`...)
	dst = appendNum(dst, p.Z)
	dst = append(dst, `,"yaw":`...)
	dst = appendNum(dst, float64(yaw))
	dst = append(dst, `,"pitch":`...)
	return appendNum(dst, float64(pitch))
}

// appendNum — число с точностью до тысячных (координаты, MSPT).
func appendNum(dst []byte, v float64) []byte {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return append(dst, '0')
	}
	return strconv.AppendFloat(dst, math.Round(v*1000)/1000, 'f', -1, 64)
}

// appendString — строка JSON с экранированием. Вход — валидный UTF-8
// (имена и чат проверены миром), невалидные байты заменяются на U+FFFD.
func appendString(dst []byte, s string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\':
				dst = append(dst, '\\', c)
			case c < 0x20:
				dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xF])
			case c == '<' || c == '>' || c == '&':
				// Как encoding/json: безопасно при вставке в HTML.
				dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xF])
			default:
				dst = append(dst, c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			dst = append(dst, `�`...)
		case r == '\u2028' || r == '\u2029':
			dst = append(dst, `\u202`...)
			dst = append(dst, hex[r&0xF])
		default:
			dst = append(dst, s[i:i+size]...)
		}
		i += size
	}
	return append(dst, '"')
}

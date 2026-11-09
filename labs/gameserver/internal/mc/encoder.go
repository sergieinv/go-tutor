package mc

import (
	"math"
	"sync/atomic"

	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/world"
)

// Encoder — world.Protocol для клиентов Minecraft. Один на всех
// Minecraft-клиентов, вызывается только из горутины тика: внутри
// переиспользуемый PacketWriter (буфер тела и zlib) и кеш готовых пакетов
// чанков.
type Encoder struct {
	// threshold — порог сжатия; задаётся при создании и не меняется,
	// поэтому его читают и горутины соединений (Set Compression, keep-alive).
	threshold  int
	pw         *mcproto.PacketWriter
	static     *chunkStatic
	sections   *sectionEncoder
	scratch    [world.SectionCount][4096]world.State
	sectionBuf []byte

	cache      map[world.ChunkPos]cachedChunk
	cacheLimit int
	// Счётчики кеша чанков: пишет тик, читает метрика из горутины
	// Prometheus — поэтому атомарные.
	hits, misses atomic.Uint64
}

type cachedChunk struct {
	version uint32
	edited  bool
	frame   []byte
}

// NewEncoder — threshold — порог сжатия (как в Set Compression; -1 — без
// сжатия). Он общий для всех соединений: пакет кодируется один раз.
func NewEncoder(threshold int) *Encoder {
	sec := newSectionEncoder()
	return &Encoder{
		threshold:  threshold,
		pw:         mcproto.NewPacketWriter(threshold),
		static:     newChunkStatic(sec),
		sections:   sec,
		cache:      make(map[world.ChunkPos]cachedChunk),
		cacheLimit: 16384,
	}
}

// Threshold — порог сжатия.
func (e *Encoder) Threshold() int { return e.threshold }

// ChunkCacheStats — попадания и промахи кеша пакетов чанков.
func (e *Encoder) ChunkCacheStats() (hits, misses uint64) { return e.hits.Load(), e.misses.Load() }

func (e *Encoder) Begin(dst []byte) []byte                    { return dst }
func (e *Encoder) End(dst []byte, _ *world.BatchStats) []byte { return dst }

// Append кодирует событие мира в пакеты Minecraft.
func (e *Encoder) Append(dst []byte, ev *world.Event) []byte {
	pw := e.pw
	switch ev.Kind {
	case world.EvWelcome:
		dst = e.appendWelcome(dst, ev)

	case world.EvRosterAdd:
		// Player Info Update: действия add_player | game_mode | listed | latency.
		b := pw.Begin(cbPlayerInfoUpdate)
		b = append(b, 0x01|0x04|0x08|0x10)
		b = mcproto.AppendVarInt(b, 1)
		b = mcproto.AppendUUID(b, ev.Player.UUID)
		b = mcproto.AppendString(b, ev.Player.Name)
		b = mcproto.AppendVarInt(b, 0) // свойств (скина) нет — клиент возьмёт скин по умолчанию
		b = mcproto.AppendVarInt(b, 1) // creative
		b = mcproto.AppendBool(b, true)
		b = mcproto.AppendVarInt(b, 0) // пинг
		dst = pw.Frame(dst, b)

	case world.EvRosterRemove:
		b := pw.Begin(cbPlayerInfoRemove)
		b = mcproto.AppendVarInt(b, 1)
		b = mcproto.AppendUUID(b, ev.Player.UUID)
		dst = pw.Frame(dst, b)

	case world.EvSpawn:
		p := ev.Player
		b := pw.Begin(cbAddEntity)
		b = mcproto.AppendVarInt(b, int32(p.ID))
		b = mcproto.AppendUUID(b, p.UUID)
		b = mcproto.AppendVarInt(b, playerEntityType)
		b = mcproto.AppendDouble(b, p.Pos.X)
		b = mcproto.AppendDouble(b, p.Pos.Y)
		b = mcproto.AppendDouble(b, p.Pos.Z)
		b = mcproto.AppendAngle(b, p.Pitch)
		b = mcproto.AppendAngle(b, p.Yaw)
		b = mcproto.AppendAngle(b, p.Yaw) // поворот головы
		b = mcproto.AppendVarInt(b, 0)    // data
		b = mcproto.AppendShort(b, 0)
		b = mcproto.AppendShort(b, 0)
		b = mcproto.AppendShort(b, 0)
		dst = pw.Frame(dst, b)

	case world.EvDespawn:
		b := pw.Begin(cbRemoveEntities)
		b = mcproto.AppendVarInt(b, 1)
		b = mcproto.AppendVarInt(b, int32(ev.Player.ID))
		dst = pw.Frame(dst, b)

	case world.EvMove:
		dst = e.appendMove(dst, ev)

	case world.EvChat:
		// Чат игроков — как системное сообщение «<имя> текст»: в offline
		// подписи сообщений не нужны, а Player Chat требует цепочки подписей.
		b := pw.Begin(cbSystemChat)
		b = mcproto.AppendTextPlain(b, "<"+ev.Player.Name+"> "+ev.Text)
		b = mcproto.AppendBool(b, false)
		dst = pw.Frame(dst, b)

	case world.EvSystem:
		b := pw.Begin(cbSystemChat)
		b = mcproto.AppendTextColored(b, ev.Text, "yellow")
		b = mcproto.AppendBool(b, false)
		dst = pw.Frame(dst, b)

	case world.EvChunkBatchBegin:
		dst = pw.Frame(dst, pw.Begin(cbChunkBatchStart))

	case world.EvChunkBatchEnd:
		b := pw.Begin(cbChunkBatchFinished)
		b = mcproto.AppendVarInt(b, int32(ev.Count))
		dst = pw.Frame(dst, b)

	case world.EvChunk:
		dst = append(dst, e.chunkFrame(ev.Chunk, ev.Column)...)

	case world.EvUnchunk:
		// Unload Chunk: с 1.20.2 сначала z, потом x (это long ChunkPos).
		b := pw.Begin(cbForgetLevelChunk)
		b = mcproto.AppendInt(b, ev.Chunk.Z)
		b = mcproto.AppendInt(b, ev.Chunk.X)
		dst = pw.Frame(dst, b)

	case world.EvCenter:
		b := pw.Begin(cbSetChunkCacheCenter)
		b = mcproto.AppendVarInt(b, ev.Chunk.X)
		b = mcproto.AppendVarInt(b, ev.Chunk.Z)
		dst = pw.Frame(dst, b)

	case world.EvBlock:
		b := pw.Begin(cbBlockUpdate)
		b = mcproto.AppendPosition(b, ev.Block.X, ev.Block.Y, ev.Block.Z)
		b = mcproto.AppendVarInt(b, int32(ev.State))
		dst = pw.Frame(dst, b)

	case world.EvBlockAck:
		b := pw.Begin(cbBlockChangedAck)
		b = mcproto.AppendVarInt(b, ev.Seq)
		dst = pw.Frame(dst, b)

	case world.EvTeleport:
		dst = e.appendPosition(dst, ev.To, ev.Yaw, ev.Pitch, ev.TeleportID)

	case world.EvKick:
		b := pw.Begin(cbDisconnect)
		b = mcproto.AppendTextPlain(b, ev.Text)
		dst = pw.Frame(dst, b)
	}
	return dst
}

func (e *Encoder) appendPosition(dst []byte, p world.Vec3, yaw, pitch float32, tp int32) []byte {
	// Synchronize Player Position: абсолютные координаты (flags = 0).
	b := e.pw.Begin(cbPlayerPosition)
	b = mcproto.AppendDouble(b, p.X)
	b = mcproto.AppendDouble(b, p.Y)
	b = mcproto.AppendDouble(b, p.Z)
	b = mcproto.AppendFloat(b, yaw)
	b = mcproto.AppendFloat(b, pitch)
	b = append(b, 0)
	b = mcproto.AppendVarInt(b, tp)
	return e.pw.Frame(dst, b)
}

// appendWelcome — вход в игру. Порядок как у ванильного сервера: Login,
// способности, точка появления, граница, время, позиция, «ждите чанки».
// Список игроков и центр зоны мир присылает следующими событиями.
func (e *Encoder) appendWelcome(dst []byte, ev *world.Event) []byte {
	pw, wl, p := e.pw, ev.Welcome, ev.Player

	b := pw.Begin(cbLogin)
	b = mcproto.AppendInt(b, int32(p.ID))
	b = mcproto.AppendBool(b, false) // hardcore
	b = mcproto.AppendVarInt(b, 1)
	b = mcproto.AppendString(b, "minecraft:overworld")
	b = mcproto.AppendVarInt(b, int32(wl.MaxPlayers))
	b = mcproto.AppendVarInt(b, int32(wl.ViewDistance))
	b = mcproto.AppendVarInt(b, int32(wl.ViewDistance)) // simulation distance
	b = mcproto.AppendBool(b, false)                    // reduced debug info
	b = mcproto.AppendBool(b, true)                     // respawn screen
	b = mcproto.AppendBool(b, false)                    // limited crafting
	b = mcproto.AppendVarInt(b, 0)                      // dimension type: overworld (id 0 в нашем реестре)
	b = mcproto.AppendString(b, "minecraft:overworld")
	b = mcproto.AppendLong(b, 0)     // hashed seed
	b = append(b, 1)                 // game mode: creative — полёт, мгновенная ломка блоков
	b = append(b, 0xFF)              // previous game mode: -1
	b = mcproto.AppendBool(b, false) // debug world
	b = mcproto.AppendBool(b, true)  // flat world: горизонт на уровне y=-64
	b = mcproto.AppendBool(b, false) // death location
	b = mcproto.AppendVarInt(b, 0)   // portal cooldown
	b = mcproto.AppendBool(b, false) // enforces secure chat — в offline подписей нет
	dst = pw.Frame(dst, b)

	b = pw.Begin(cbPlayerAbilities)
	b = append(b, 0x01|0x04|0x08) // invulnerable | allow flying | creative
	b = mcproto.AppendFloat(b, 0.05)
	b = mcproto.AppendFloat(b, 0.1)
	dst = pw.Frame(dst, b)

	b = pw.Begin(cbSetDefaultSpawn)
	b = mcproto.AppendPosition(b, int32(math.Floor(wl.Spawn.X)), int32(wl.Spawn.Y), int32(math.Floor(wl.Spawn.Z)))
	b = mcproto.AppendFloat(b, 0)
	dst = pw.Frame(dst, b)

	b = pw.Begin(cbInitializeBorder)
	b = mcproto.AppendDouble(b, 0)
	b = mcproto.AppendDouble(b, 0)
	b = mcproto.AppendDouble(b, wl.BorderRadius*2)
	b = mcproto.AppendDouble(b, wl.BorderRadius*2)
	b = mcproto.AppendVarLong(b, 0)
	b = mcproto.AppendVarInt(b, 29999984)
	b = mcproto.AppendVarInt(b, 5)
	b = mcproto.AppendVarInt(b, 15)
	dst = pw.Frame(dst, b)

	// Время суток: отрицательное значение останавливает цикл — вечный полдень.
	b = pw.Begin(cbSetTime)
	b = mcproto.AppendLong(b, 0)
	b = mcproto.AppendLong(b, -6000)
	dst = pw.Frame(dst, b)

	dst = e.appendPosition(dst, p.Pos, p.Yaw, p.Pitch, ev.TeleportID)

	// Game Event 13 «start waiting for level chunks»: без него клиент
	// 1.20.3+ так и висит на экране «Загрузка ландшафта».
	b = pw.Begin(cbGameEvent)
	b = append(b, 13)
	b = mcproto.AppendFloat(b, 0)
	return pw.Frame(dst, b)
}

// appendMove выбирает пакет перемещения. Дельты передаются в 1/4096
// блока в int16, то есть не дальше 8 блоков; дальше — абсолютный телепорт.
// Дельта считается между округлёнными позициями, поэтому ошибка
// округления не копится на клиенте.
func (e *Encoder) appendMove(dst []byte, ev *world.Event) []byte {
	pw := e.pw
	id := int32(ev.Player.ID)
	dx := fixed(ev.To.X) - fixed(ev.From.X)
	dy := fixed(ev.To.Y) - fixed(ev.From.Y)
	dz := fixed(ev.To.Z) - fixed(ev.From.Z)
	far := !fitsShort(dx) || !fitsShort(dy) || !fitsShort(dz)
	switch {
	case ev.Moved && far:
		b := pw.Begin(cbTeleportEntity)
		b = mcproto.AppendVarInt(b, id)
		b = mcproto.AppendDouble(b, ev.To.X)
		b = mcproto.AppendDouble(b, ev.To.Y)
		b = mcproto.AppendDouble(b, ev.To.Z)
		b = mcproto.AppendAngle(b, ev.Yaw)
		b = mcproto.AppendAngle(b, ev.Pitch)
		b = mcproto.AppendBool(b, ev.OnGround)
		dst = pw.Frame(dst, b)
	case ev.Moved && ev.Rotated:
		b := pw.Begin(cbMoveEntityPosRot)
		b = mcproto.AppendVarInt(b, id)
		b = mcproto.AppendShort(b, int16(dx))
		b = mcproto.AppendShort(b, int16(dy))
		b = mcproto.AppendShort(b, int16(dz))
		b = mcproto.AppendAngle(b, ev.Yaw)
		b = mcproto.AppendAngle(b, ev.Pitch)
		b = mcproto.AppendBool(b, ev.OnGround)
		dst = pw.Frame(dst, b)
	case ev.Moved:
		b := pw.Begin(cbMoveEntityPos)
		b = mcproto.AppendVarInt(b, id)
		b = mcproto.AppendShort(b, int16(dx))
		b = mcproto.AppendShort(b, int16(dy))
		b = mcproto.AppendShort(b, int16(dz))
		b = mcproto.AppendBool(b, ev.OnGround)
		dst = pw.Frame(dst, b)
	case ev.Rotated:
		b := pw.Begin(cbMoveEntityRot)
		b = mcproto.AppendVarInt(b, id)
		b = mcproto.AppendAngle(b, ev.Yaw)
		b = mcproto.AppendAngle(b, ev.Pitch)
		b = mcproto.AppendBool(b, ev.OnGround)
		dst = pw.Frame(dst, b)
	}
	if ev.Rotated {
		// Голова поворачивается отдельным пакетом — иначе тело
		// развернётся, а голова останется смотреть в старую сторону.
		b := pw.Begin(cbRotateHead)
		b = mcproto.AppendVarInt(b, id)
		b = mcproto.AppendAngle(b, ev.Yaw)
		dst = pw.Frame(dst, b)
	}
	return dst
}

func fixed(v float64) int64 { return int64(math.Round(v * 4096)) }

func fitsShort(v int64) bool { return v >= math.MinInt16 && v <= math.MaxInt16 }

// chunkFrame возвращает готовый (сжатый) кадр чанка из кеша или кодирует
// его. Нетронутые чанки отличаются только координатами, но координаты
// внутри пакета — поэтому кеш по позиции.
func (e *Encoder) chunkFrame(pos world.ChunkPos, col *world.Column) []byte {
	var version uint32
	if col != nil {
		version = col.Version
	}
	if c, ok := e.cache[pos]; ok && c.version == version && c.edited == (col != nil) {
		e.hits.Add(1)
		return c.frame
	}
	e.misses.Add(1)
	b := e.pw.Begin(cbLevelChunkWithLight)
	b = e.appendChunkBody(b, pos, col)
	frame := e.pw.Frame(nil, b)
	if len(e.cache) >= e.cacheLimit {
		clear(e.cache) // простая защита от роста: мир ограничен границей, сюда не дойдём
	}
	e.cache[pos] = cachedChunk{version: version, edited: col != nil, frame: frame}
	return frame
}

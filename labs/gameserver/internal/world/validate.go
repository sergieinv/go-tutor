package world

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Проверка ввода. Клиенту не доверяем ни в чём: он присылает желаемое
// («я теперь здесь», «я сломал этот блок»), а сервер решает, возможно ли
// это. Одни и те же правила — для Minecraft и для браузера.

// Причины отклонения — значения метки reason метрики отклонённого ввода.
const (
	rejectTooFast     = "too_fast"
	rejectOutOfBounds = "out_of_bounds"
	rejectNotFinite   = "not_finite"
	rejectChatRate    = "chat_rate"
	rejectChatInvalid = "chat_invalid"
	rejectBlockRate   = "block_rate"
	rejectBlockReach  = "block_reach"
	rejectBlockState  = "block_state"
	rejectBlockLimit  = "block_limit"
)

// RejectReasons — все причины отклонения ввода (для метрик).
var RejectReasons = []string{rejectTooFast, rejectOutOfBounds, rejectNotFinite, rejectChatRate,
	rejectChatInvalid, rejectBlockRate, rejectBlockReach, rejectBlockState, rejectBlockLimit}

// eyeHeight — высота глаз игрока над ногами.
const eyeHeight = 1.62

// validName — ник Minecraft: 3–16 символов [A-Za-z0-9_]. То же правило
// для браузерных игроков: их видят клиенты Minecraft.
func validName(s string) bool {
	if len(s) < 3 || len(s) > 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// ValidName экспортирует правило для слоя соединений (отказ ещё до входа).
func ValidName(s string) bool { return validName(s) }

func (w *World) inBounds(v Vec3) bool {
	r := w.cfg.BorderRadius
	return v.X >= -r && v.X <= r && v.Z >= -r && v.Z <= r && v.Y >= MinY-64 && v.Y <= MaxY+64
}

// move проверяет и применяет перемещение.
func (w *World) move(p *Player, c *Command) {
	if c.HasRot {
		yaw, pitch := float64(c.Yaw), float64(c.Pitch)
		if math.IsNaN(yaw) || math.IsInf(yaw, 0) || math.IsNaN(pitch) || math.IsInf(pitch, 0) {
			w.metrics.RejectedInput(rejectNotFinite)
			w.kick(p, "Invalid rotation (NaN/Inf)", ReasonBadInput)
			return
		}
	}
	if c.HasPos && !c.Pos.finite() {
		// NaN в координатах — не ошибка клиента, а попытка эксплойта:
		// NaN «проходит» любые сравнения. Отключаем.
		w.metrics.RejectedInput(rejectNotFinite)
		w.kick(p, "Invalid position (NaN/Inf)", ReasonBadInput)
		return
	}
	if p.pendingTP != 0 {
		return // ждём подтверждения телепорта: перемещения «из прошлого» не считаем
	}
	if c.HasRot {
		yaw := float32(math.Mod(float64(c.Yaw), 360))
		if yaw < 0 {
			yaw += 360
		}
		pitch := min(max(c.Pitch, -90), 90)
		if yaw != p.Yaw || pitch != p.Pitch {
			p.Yaw, p.Pitch = yaw, pitch
			p.dirtyRot = true
		}
	}
	p.onGround = c.OnGround
	if !c.HasPos || c.Pos == p.Pos {
		return
	}
	if !w.inBounds(c.Pos) {
		w.metrics.RejectedInput(rejectOutOfBounds)
		w.teleport(p, rejectOutOfBounds)
		return
	}
	// Скорость: «бюджет» пути пополняется на MaxSpeed/20 блока за тик и
	// копится не больше чем на секунду. Так переживаем сетевой джиттер (два
	// пакета в одном тике), но не телепортацию. Падение вниз не
	// ограничиваем — его ограничивает пол мира.
	refill := float64(w.tick-p.budgetTick) * w.cfg.MaxSpeed / TickRate
	p.moveBudget = min(w.cfg.MaxSpeed, p.moveBudget+refill)
	p.budgetTick = w.tick
	dx, dz := c.Pos.X-p.Pos.X, c.Pos.Z-p.Pos.Z
	dy := max(c.Pos.Y-p.Pos.Y, 0)
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d > p.moveBudget {
		w.metrics.RejectedInput(rejectTooFast)
		w.teleport(p, rejectTooFast)
		return
	}
	p.moveBudget -= d
	p.Pos = c.Pos
	p.dirtyPos = true
}

// teleport возвращает игрока на последнюю принятую позицию.
func (w *World) teleport(p *Player, reason string) {
	if reason != "resend" {
		p.nextTP++
		if p.nextTP <= 0 {
			p.nextTP = 1
		}
		p.pendingTP = p.nextTP
	}
	p.tpSentTick = w.tick
	w.emit(outItem{ev: Event{
		Kind: EvTeleport, Player: &p.PlayerInfo, To: p.Pos, Yaw: p.Yaw, Pitch: p.Pitch,
		TeleportID: p.pendingTP, Reason: reason,
	}, aud: toOne, target: p})
}

// CleanChat проверяет сообщение: валидный UTF-8, без управляющих
// символов и символа форматирования «§», не длиннее max символов.
// Возвращает очищенный от пробелов по краям текст.
func CleanChat(s string, maxChars int) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxChars {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '§' || r == '\u2028' || r == '\u2029' {
			return "", false
		}
	}
	return s, true
}

func (w *World) chat(p *Player, text string) {
	if !p.chat.allow(w.tick, w.cfg.ChatPerSecond/TickRate, w.cfg.ChatBurst) {
		w.metrics.RejectedInput(rejectChatRate)
		w.emit(outItem{ev: Event{Kind: EvSystem, Text: "Too many messages, slow down"}, aud: toOne, target: p})
		return
	}
	msg, ok := CleanChat(text, w.cfg.MaxChat)
	if !ok {
		w.metrics.RejectedInput(rejectChatInvalid)
		w.emit(outItem{ev: Event{Kind: EvSystem, Text: "Message rejected: empty, too long or has control characters"}, aud: toOne, target: p})
		return
	}
	// Чат — тоже через interest management: сообщение получают те, кто
	// видит автора, и сам автор.
	w.emit(outItem{ev: Event{Kind: EvChat, Player: &p.PlayerInfo, Text: msg}, aud: toViewers, self: true})
}

// placeable — блоки, которые может ставить браузерный клиент. Игрок
// Minecraft ставит то, что держит в руке (из креативного инвентаря).
var placeable = []string{
	"minecraft:stone", "minecraft:cobblestone", "minecraft:dirt", "minecraft:grass_block",
	"minecraft:oak_planks", "minecraft:bricks", "minecraft:stone_bricks", "minecraft:glass",
	"minecraft:white_wool", "minecraft:red_wool", "minecraft:blue_wool", "minecraft:yellow_wool",
	"minecraft:gold_block", "minecraft:diamond_block",
}

// PlaceableNames — список для welcome браузерного клиента.
func PlaceableNames() []string { return placeable }

// blockAction — общие проверки действия с блоком.
func (w *World) blockAction(p *Player, b BlockPos) (string, bool) {
	if !p.blocks.allow(w.tick, w.cfg.BlocksPerSecond/TickRate, w.cfg.BlockBurst) {
		return rejectBlockRate, false
	}
	if b.Y <= MinY || b.Y > MaxY || math.Abs(float64(b.X)) > w.cfg.BorderRadius || math.Abs(float64(b.Z)) > w.cfg.BorderRadius {
		return rejectOutOfBounds, false
	}
	if !p.loaded[ChunkPos{b.X >> 4, b.Z >> 4}] {
		return rejectBlockReach, false
	}
	// Дальность: от глаз до центра блока.
	ex, ey, ez := p.Pos.X, p.Pos.Y+eyeHeight, p.Pos.Z
	dx, dy, dz := float64(b.X)+0.5-ex, float64(b.Y)+0.5-ey, float64(b.Z)+0.5-ez
	if p.Kind == KindWeb {
		dy = 0 // у карты сверху нет высоты взгляда: меряем по горизонтали
	}
	if dx*dx+dy*dy+dz*dz > w.cfg.Reach*w.cfg.Reach {
		return rejectBlockReach, false
	}
	return "", true
}

func (w *World) dig(p *Player, b BlockPos, seq int32) {
	reason, ok := w.blockAction(p, b)
	if ok && IsAir(w.chunks.Block(b.X, b.Y, b.Z)) {
		reason, ok = rejectBlockState, false
	}
	w.applyBlock(p, b, Air, seq, reason, ok)
}

func (w *World) place(p *Player, b BlockPos, s State, seq int32) {
	reason, ok := w.blockAction(p, b)
	if ok && (int(s) >= StateCount() || IsAir(s) || !IsAir(w.chunks.Block(b.X, b.Y, b.Z))) {
		reason, ok = rejectBlockState, false
	}
	if ok && p.Kind == KindWeb && !isPlaceable(s) {
		reason, ok = rejectBlockState, false
	}
	w.applyBlock(p, b, s, seq, reason, ok)
}

func isPlaceable(s State) bool {
	name := StateName(s)
	for _, n := range placeable {
		if n == name {
			return true
		}
	}
	return false
}

func (w *World) applyBlock(p *Player, b BlockPos, s State, seq int32, reason string, ok bool) {
	// Общий предел правок: без него игроки (или боты) за часы набрали бы
	// миллионы правок, а снапшот копирует их все.
	if ok && w.cfg.MaxEdits > 0 && w.chunks.Edits() >= w.cfg.MaxEdits && w.chunks.AddsEdit(b.X, b.Y, b.Z, s) {
		reason, ok = rejectBlockLimit, false
		w.emit(outItem{ev: Event{Kind: EvSystem, Text: "World edit limit reached"}, aud: toOne, target: p})
	}
	if ok && w.chunks.Set(b.X, b.Y, b.Z, s) {
		col := w.chunks.Column(ChunkPos{b.X >> 4, b.Z >> 4})
		ty, ts := TopBlock(col, b.X, b.Z)
		w.emit(outItem{ev: Event{Kind: EvBlock, Block: b, State: s, TopY: ty, TopState: ts}, aud: toChunk})
	} else if !ok {
		w.metrics.RejectedInput(reason)
		// Клиент Minecraft уже нарисовал свой результат (предсказание):
		// присылаем настоящее состояние блока, чтобы он откатился.
		real := w.chunks.Block(b.X, b.Y, b.Z)
		col := w.chunks.Column(ChunkPos{b.X >> 4, b.Z >> 4})
		ty, ts := TopBlock(col, b.X, b.Z)
		w.emit(outItem{ev: Event{Kind: EvBlock, Block: b, State: real, TopY: ty, TopState: ts}, aud: toOne, target: p})
	}
	w.emit(outItem{ev: Event{Kind: EvBlockAck, Seq: seq}, aud: toOne, target: p})
}

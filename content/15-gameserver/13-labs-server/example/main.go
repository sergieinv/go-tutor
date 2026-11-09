// Сервер из labs в миниатюре: одна горутина-владелец мира, игроки
// двух видов клиентов (Minecraft и браузер), нейтральные события и
// по кодировщику на протокол. Широковещательное событие кодируется
// один раз на протокол и копируется в пачки всех получателей;
// каждый клиент получает одну пачку за тик через неблокирующую
// отправку. Так устроены `world.Step` и `broadcast` в
// `labs/gameserver/internal/world/world.go`.
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

type Vec struct{ X, Z float64 }

type EventKind int

const (
	EvMove EventKind = iota + 1
	EvChat
)

// Событие мира не знает ни Minecraft, ни JSON: «игрок ID
// переместился из From в To», «игрок написал Text».
type Event struct {
	Kind     EventKind
	ID       int32
	Name     string
	From, To Vec
	Text     string
}

// Протокол превращает события в байты. Один экземпляр обслуживает
// всех клиентов своего типа и вызывается только из тика, поэтому
// внутри можно держать буферы без блокировок.
type Protocol interface {
	Name() string
	Begin(dst []byte) []byte
	Append(dst []byte, ev *Event) []byte
	End(dst []byte, tick uint64, events int) []byte
}

// Minecraft: кадр «VarInt длина → VarInt id → поля». Перемещение —
// дельта в 1/4096 блока в `int16` (Update Entity Position, 0x2E в
// протоколе 767), то есть не дальше 8 блоков; дальше — абсолютный
// телепорт (0x70). Чат упрощён: в 1.21.1 текст — NBT-компонент.
type mcEncoder struct {
	body    []byte
	encodes int
}

func (e *mcEncoder) Name() string                           { return "minecraft" }
func (e *mcEncoder) Begin(dst []byte) []byte                { return dst }
func (e *mcEncoder) End(dst []byte, _ uint64, _ int) []byte { return dst }

func (e *mcEncoder) Append(dst []byte, ev *Event) []byte {
	e.encodes++
	b := e.body[:0]
	switch ev.Kind {
	case EvMove:
		dx := fixed(ev.To.X) - fixed(ev.From.X)
		dz := fixed(ev.To.Z) - fixed(ev.From.Z)
		if fitsShort(dx) && fitsShort(dz) {
			b = appendVarInt(b, 0x2E)
			b = appendVarInt(b, ev.ID)
			b = binary.BigEndian.AppendUint16(b, uint16(dx))
			b = binary.BigEndian.AppendUint16(b, 0) // dy: вид сверху
			b = binary.BigEndian.AppendUint16(b, uint16(dz))
			b = append(b, 1) // on ground
		} else {
			b = appendVarInt(b, 0x70)
			b = appendVarInt(b, ev.ID)
			b = binary.BigEndian.AppendUint64(b, math.Float64bits(ev.To.X))
			b = binary.BigEndian.AppendUint64(b, math.Float64bits(-60))
			b = binary.BigEndian.AppendUint64(b, math.Float64bits(ev.To.Z))
			b = append(b, 0, 0, 1) // yaw, pitch, on ground
		}
	case EvChat:
		b = appendVarInt(b, 0x6C)
		b = appendString(b, "<"+ev.Name+"> "+ev.Text)
		b = append(b, 0) // не над панелью предметов
	}
	e.body = b
	dst = appendVarInt(dst, int32(len(b)))
	return append(dst, b...)
}

// Браузер: JSON-массив событий тика, последним — `tick`. Его
// получает каждый клиент каждый тик: по нему рисуется график MSPT.
type webEncoder struct{ encodes int }

func (e *webEncoder) Name() string            { return "web" }
func (e *webEncoder) Begin(dst []byte) []byte { return append(dst, '[') }

func (e *webEncoder) Append(dst []byte, ev *Event) []byte {
	e.encodes++
	switch ev.Kind {
	case EvMove:
		dst = fmt.Appendf(dst, `{"t":"move","id":%d,"x":`, ev.ID)
		dst = strconv.AppendFloat(dst, ev.To.X, 'f', -1, 64)
		dst = append(dst, `,"z":`...)
		dst = strconv.AppendFloat(dst, ev.To.Z, 'f', -1, 64)
		dst = append(dst, "},"...)
	case EvChat:
		text, _ := json.Marshal(ev.Text) // в labs — свой append без рефлексии
		dst = fmt.Appendf(dst, `{"t":"chat","id":%d,"text":%s},`, ev.ID, text)
	}
	return dst
}

func (e *webEncoder) End(dst []byte, tick uint64, events int) []byte {
	return fmt.Appendf(dst, `{"t":"tick","n":%d,"msgs":%d}]`, tick, events)
}

// Сторона соединения, которую видит мир: протокол и ограниченная
// очередь пачек. Горутина записи в этом примере — сам `main`.
type Peer struct {
	proto  Protocol
	out    chan []byte
	reason string
}

func (p *Peer) trySend(b []byte) bool {
	select {
	case p.out <- b:
		return true
	default:
		return false
	}
}

// Игрок принадлежит горутине тика: мьютексов нет. Перемещение
// рассылается дельтой от `sent` — от того, что клиенты уже знают.
type Player struct {
	ID        int32
	Name      string
	Pos, sent Vec // sent — позиция, которую уже разослали
	peer      *Peer
	batch     []byte
	events    int
	moved     bool
}

type outItem struct {
	ev      Event
	subject *Player
	self    bool // и самому автору (чат)
}

// Кеш кодирования: по записи на протокол, сбрасывается перед каждым
// событием. Протоколов два, поэтому это срез, а не map.
type cacheEntry struct {
	proto Protocol
	buf   []byte
	valid bool
}

type World struct {
	tick       uint64
	list       []*Player // порядок входа: детерминированный обход
	out        []outItem
	cache      []cacheEntry
	kicks      []*Player
	deliveries int
	log        []string // что случилось в тике — для вывода примера
}

const viewDistance = 2 // в чанках, квадрат (2r+1)²

func chunk(v float64) int { return int(math.Floor(v)) >> 4 }

func (w *World) sees(a, b *Player) bool {
	dx := chunk(a.Pos.X) - chunk(b.Pos.X)
	dz := chunk(a.Pos.Z) - chunk(b.Pos.Z)
	return a != b && max(dx, -dx) <= viewDistance && max(dz, -dz) <= viewDistance
}

// Шаг тика. Ввод в реальном сервере приходит из ограниченного канала
// `inbox`; здесь — функцией, чтобы вывод не зависел от планировщика.
func (w *World) Step(input func(w *World)) {
	w.tick++
	for _, p := range w.kicks { // отключённые в прошлом тике
		w.list = slices.DeleteFunc(w.list, func(q *Player) bool { return q == p })
		w.log = append(w.log, p.Name+": убран из мира в начале тика")
	}
	w.kicks = w.kicks[:0]
	if input != nil {
		input(w)
	}
	w.broadcast()
}

func (w *World) move(p *Player, to Vec) { p.Pos, p.moved = to, true }

func (w *World) chat(p *Player, text string) {
	w.out = append(w.out, outItem{ev: Event{Kind: EvChat, ID: p.ID, Name: p.Name, Text: text}, subject: p, self: true})
}

// Рассылка: пачка на игрока, события по порядку, кодирование —
// не больше одного раза на протокол для каждого события.
func (w *World) broadcast() {
	for _, p := range w.list {
		p.batch = p.peer.proto.Begin(nil)
		p.events = 0
	}
	for _, p := range w.list { // перемещения — тем, кто видит
		if p.moved {
			w.out = append(w.out, outItem{ev: Event{Kind: EvMove, ID: p.ID, From: p.sent, To: p.Pos}, subject: p})
			p.sent, p.moved = p.Pos, false
		}
	}
	for i := range w.out {
		it := &w.out[i]
		w.resetCache()
		for _, q := range w.list {
			if w.sees(q, it.subject) || it.self && q == it.subject {
				w.appendEncoded(q, &it.ev)
			}
		}
	}
	w.out = w.out[:0]
	for _, p := range w.list {
		b := p.peer.proto.End(p.batch, w.tick, p.events)
		if len(b) == 0 {
			continue // Minecraft-клиенту без событий слать нечего
		}
		if !p.peer.trySend(b) {
			// Очередь полна: тик не ждёт, клиент отключается.
			p.peer.reason = "slow_consumer"
			w.kicks = append(w.kicks, p)
			w.log = append(w.log, p.Name+": очередь полна — отключение "+p.peer.reason)
		}
	}
}

func (w *World) resetCache() {
	for i := range w.cache {
		w.cache[i].valid = false
	}
}

// Первый получатель с этим протоколом кодирует событие, остальные
// копируют готовые байты.
func (w *World) appendEncoded(q *Player, ev *Event) {
	proto := q.peer.proto
	var c *cacheEntry
	for i := range w.cache {
		if w.cache[i].proto == proto {
			c = &w.cache[i]
		}
	}
	if c == nil {
		w.cache = append(w.cache, cacheEntry{proto: proto})
		c = &w.cache[len(w.cache)-1]
	}
	if !c.valid {
		c.buf = proto.Append(c.buf[:0], ev)
		c.valid = true
	}
	q.batch = append(q.batch, c.buf...)
	q.events++
	w.deliveries++
}

// Пять игроков: четверо рядом, Dave далеко, у Eve очередь
// на одну пачку, которую никто не читает.
func main() {
	mc, web := &mcEncoder{}, &webEncoder{}
	w := &World{}
	join := func(name string, proto Protocol, queue int, pos Vec) *Player {
		p := &Player{ID: int32(len(w.list) + 1), Name: name, Pos: pos, sent: pos,
			peer: &Peer{proto: proto, out: make(chan []byte, queue)}}
		w.list = append(w.list, p)
		return p
	}
	alice := join("Alice", mc, 64, Vec{1.5, 0.5})
	bob := join("Bob", web, 64, Vec{4, 2})
	carol := join("Carol", mc, 64, Vec{-3, 5})
	join("Dave", mc, 64, Vec{200, 0}) // далеко: в зоне видимости никого
	join("Eve", web, 1, Vec{6, -6})   // вкладка «уснула»: очередь на 1 пачку не читается

	ticks := []func(w *World){
		func(w *World) {
			w.move(alice, Vec{3.5, 0.5}) // 2 блока — дельта
			w.move(bob, Vec{24, 2})      // 20 блоков — телепорт для Minecraft
			w.chat(carol, "привет")
		},
		func(w *World) { w.move(alice, Vec{4, 1}) },
		nil,
	}
	for _, input := range ticks {
		mc.encodes, web.encodes, w.deliveries, w.log = 0, 0, 0, w.log[:0]
		w.Step(input)
		// Без кеша кодирований было бы столько же, сколько доставок.
		fmt.Printf("тик %d: доставок %d, кодирований minecraft=%d web=%d\n",
			w.tick, w.deliveries, mc.encodes, web.encodes)
		for _, l := range w.log {
			fmt.Println("  " + l)
		}
		// Горутины записи: вычитать очереди (кроме уснувшей Eve).
		for _, p := range w.list {
			if p.Name == "Eve" {
				continue
			}
			select {
			case b := <-p.peer.out:
				fmt.Printf("  %-5s ← %s\n", p.Name, describe(p.peer.proto, b))
			default:
				fmt.Printf("  %-5s ← (пачки нет)\n", p.Name)
			}
		}
	}
}

// describe показывает пачку: для Minecraft — id и длины кадров,
// для браузера — JSON как есть.
func describe(proto Protocol, b []byte) string {
	if proto.Name() == "web" {
		return string(b)
	}
	names := map[int32]string{0x2E: "move", 0x70: "teleport", 0x6C: "chat"}
	var parts []string
	for len(b) > 0 {
		n, k := readVarInt(b)
		id, _ := readVarInt(b[k:])
		parts = append(parts, fmt.Sprintf("0x%02X %s (%d байт)", id, names[id], k+int(n)))
		b = b[k+int(n):]
	}
	return strings.Join(parts, " | ")
}

func fixed(v float64) int64  { return int64(math.Round(v * 4096)) }
func fitsShort(v int64) bool { return v >= math.MinInt16 && v <= math.MaxInt16 }
func appendString(b []byte, s string) []byte {
	return append(appendVarInt(b, int32(len(s))), s...)
}

func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func readVarInt(b []byte) (int32, int) {
	var u uint32
	for i := 0; i < 5 && i < len(b); i++ {
		u |= uint32(b[i]&0x7F) << (7 * i)
		if b[i]&0x80 == 0 {
			return int32(u), i + 1
		}
	}
	return 0, len(b)
}

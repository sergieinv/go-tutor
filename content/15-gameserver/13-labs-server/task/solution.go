package task

import (
	"errors"
	"slices"
)

// PlayerID — идентификатор игрока (больше нуля).
type PlayerID int32

// Event — нейтральное событие мира: протоколы превращают его в байты.
type Event struct {
	Kind    string
	Subject PlayerID
	Text    string
}

// Stats — статистика пачки одного игрока за тик.
type Stats struct {
	Tick   uint64
	Events int
}

// Protocol кодирует события в байты своего протокола. Один экземпляр
// обслуживает всех клиентов своего типа.
type Protocol interface {
	Begin(dst []byte) []byte
	Append(dst []byte, ev *Event) []byte
	End(dst []byte, st Stats) []byte
}

// Audience — кому адресовано событие.
type Audience uint8

const (
	ToOne     Audience = iota + 1 // одному игроку Target
	ToAll                         // всем, кроме Exclude
	ToViewers                     // тем, кто видит Subject (и самому Subject, если Self)
)

// Item — событие с адресатами.
type Item struct {
	Event   Event
	To      Audience
	Target  PlayerID
	Exclude PlayerID
	Self    bool
}

// Batch — пачка байт одного игрока за тик.
type Batch struct {
	Player PlayerID
	Data   []byte
}

var (
	ErrBadPlayer     = errors.New("bad player")
	ErrDuplicate     = errors.New("player already added")
	ErrUnknownPlayer = errors.New("unknown player")
)

type player struct {
	id      PlayerID
	proto   Protocol
	visible map[PlayerID]bool
	batch   []byte
	events  int
}

type cacheEntry struct {
	proto Protocol
	buf   []byte
	valid bool
}

// Hub собирает события тика и раздаёт их игрокам пачками.
type Hub struct {
	players map[PlayerID]*player
	items   []Item
	cache   []cacheEntry
}

// NewHub возвращает пустой Hub.
func NewHub() *Hub {
	return &Hub{players: make(map[PlayerID]*player)}
}

// Add добавляет игрока с протоколом p.
func (h *Hub) Add(id PlayerID, p Protocol) error {
	if id <= 0 || p == nil {
		return ErrBadPlayer
	}
	if h.players[id] != nil {
		return ErrDuplicate
	}
	h.players[id] = &player{id: id, proto: p, visible: make(map[PlayerID]bool)}
	return nil
}

// Remove удаляет игрока и все пары видимости с ним.
func (h *Hub) Remove(id PlayerID) bool {
	p := h.players[id]
	if p == nil {
		return false
	}
	for q := range p.visible {
		delete(h.players[q].visible, id)
	}
	delete(h.players, id)
	return true
}

// SetVisible включает или выключает взаимную видимость a и b.
func (h *Hub) SetVisible(a, b PlayerID, visible bool) error {
	if a == b {
		return ErrBadPlayer
	}
	pa, pb := h.players[a], h.players[b]
	if pa == nil || pb == nil {
		return ErrUnknownPlayer
	}
	if visible {
		pa.visible[b], pb.visible[a] = true, true
	} else {
		delete(pa.visible, b)
		delete(pb.visible, a)
	}
	return nil
}

// Emit ставит событие в очередь тика.
func (h *Hub) Emit(it Item) { h.items = append(h.items, it) }

// Flush раздаёт накопленные события и возвращает непустые пачки по
// возрастанию id игрока.
func (h *Hub) Flush(tick uint64) []Batch {
	for _, p := range h.players {
		p.batch = p.proto.Begin(make([]byte, 0, 64))
		p.events = 0
	}
	for i := range h.items {
		it := &h.items[i]
		ev := it.Event // копия: протокол не меняет очередь
		switch it.To {
		case ToOne:
			if p := h.players[it.Target]; p != nil {
				n := len(p.batch)
				p.batch = p.proto.Append(p.batch, &ev)
				if len(p.batch) > n {
					p.events++
				}
			}
		case ToAll:
			h.resetCache()
			for id, p := range h.players {
				if id != it.Exclude {
					h.appendEncoded(p, &ev)
				}
			}
		case ToViewers:
			src := h.players[ev.Subject]
			if src == nil {
				continue
			}
			h.resetCache()
			if it.Self {
				h.appendEncoded(src, &ev)
			}
			for id := range src.visible {
				h.appendEncoded(h.players[id], &ev)
			}
		}
	}
	clear(h.items)
	h.items = h.items[:0]

	var out []Batch
	for id, p := range h.players {
		b := p.proto.End(p.batch, Stats{Tick: tick, Events: p.events})
		p.batch = nil
		if len(b) > 0 {
			out = append(out, Batch{Player: id, Data: b})
		}
	}
	slices.SortFunc(out, func(a, b Batch) int { return int(a.Player) - int(b.Player) })
	return out
}

func (h *Hub) resetCache() {
	for i := range h.cache {
		h.cache[i].valid = false
	}
}

// appendEncoded кодирует событие не больше одного раза на протокол.
func (h *Hub) appendEncoded(p *player, ev *Event) {
	var c *cacheEntry
	for i := range h.cache {
		if h.cache[i].proto == p.proto {
			c = &h.cache[i]
			break
		}
	}
	if c == nil {
		h.cache = append(h.cache, cacheEntry{proto: p.proto})
		c = &h.cache[len(h.cache)-1]
	}
	if !c.valid {
		c.buf = p.proto.Append(c.buf[:0], ev)
		c.valid = true
	}
	if len(c.buf) > 0 {
		p.batch = append(p.batch, c.buf...)
		p.events++
	}
}

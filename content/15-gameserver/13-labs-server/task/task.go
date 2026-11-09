package task

import "errors"

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

// Hub собирает события тика и раздаёт их игрокам пачками.
type Hub struct {
	// TODO: игроки (протокол, кого видит), очередь событий тика,
	// кеш кодирования «протокол → байты текущего события»
}

// NewHub возвращает пустой Hub.
func NewHub() *Hub {
	return &Hub{}
}

// Add добавляет игрока с протоколом p.
func (h *Hub) Add(id PlayerID, p Protocol) error {
	// TODO
	return nil
}

// Remove удаляет игрока и все пары видимости с ним.
func (h *Hub) Remove(id PlayerID) bool {
	// TODO
	return false
}

// SetVisible включает или выключает взаимную видимость a и b.
func (h *Hub) SetVisible(a, b PlayerID, visible bool) error {
	// TODO
	return nil
}

// Emit ставит событие в очередь тика.
func (h *Hub) Emit(it Item) {
	// TODO
}

// Flush раздаёт накопленные события и возвращает непустые пачки по
// возрастанию id игрока.
func (h *Hub) Flush(tick uint64) []Batch {
	// TODO: Begin каждому, события по порядку (широковещательные —
	// одно кодирование на протокол), End каждому, пустые пачки — мимо
	return nil
}

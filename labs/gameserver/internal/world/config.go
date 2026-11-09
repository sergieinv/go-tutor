package world

import (
	"cmp"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// Частота тиков — как у Minecraft: 20 тиков в секунду, тик — 50 мс.
const (
	TickRate     = 20
	TickInterval = time.Second / TickRate
)

// Config — параметры мира. Значения по умолчанию — DefaultConfig.
type Config struct {
	ViewDistance       int     // радиус зоны видимости, чанков (квадрат (2R+1)²)
	MaxPlayers         int     // лимит игроков онлайн
	BorderRadius       float64 // мир — квадрат [-R, R] по x и z, блоков
	MaxSpeed           float64 // предел скорости, блоков/с (полёт в креативе ≈ 22)
	Reach              float64 // дальность взаимодействия с блоками, блоков
	ChunksPerTick      int     // сколько чанков отправлять игроку за тик
	MaxCommandsPerTick int     // сколько команд разбирать за тик (остальные — в следующем)
	InboxSize          int     // ёмкость входящего канала
	SaveEveryTicks     int     // период снапшота, тиков (0 — только при остановке)
	ChatPerSecond      float64 // лимит чата: пополнение…
	ChatBurst          float64 // …и запас
	BlocksPerSecond    float64 // лимит действий с блоками
	BlockBurst         float64
	MaxChat            int    // длина сообщения, символов (как в Minecraft — 256)
	MaxOffline         int    // сколько позиций вышедших игроков помнить
	MaxEdits           int    // предел правок блоков во всём мире (память и время снапшота)
	Seed               uint64 // зерно случайного разброса точки появления
}

// DefaultConfig — настройки по умолчанию.
func DefaultConfig() Config {
	return Config{
		ViewDistance:       6,
		MaxPlayers:         256,
		BorderRadius:       512,
		MaxSpeed:           30,
		Reach:              6,
		ChunksPerTick:      8,
		MaxCommandsPerTick: 20000,
		InboxSize:          8192,
		SaveEveryTicks:     30 * TickRate,
		ChatPerSecond:      1,
		ChatBurst:          5,
		BlocksPerSecond:    10,
		BlockBurst:         20,
		MaxChat:            256,
		MaxOffline:         10000,
		MaxEdits:           100000,
		Seed:               1,
	}
}

// Metrics — что мир сообщает наружу. Реализация на Prometheus — в
// internal/metrics; в тестах — NopMetrics или запись в память. Мир не
// зависит от библиотеки метрик.
type Metrics interface {
	TickDone(d time.Duration)
	TicksSkipped(n int)
	TPS(tps float64)
	Players(kind Kind, n int)
	Disconnect(reason string)
	JoinRejected(reason string)
	RejectedInput(reason string)
	ChunksSent(n int)
	InboxLen(n int)
}

// NopMetrics ничего не делает.
type NopMetrics struct{}

func (NopMetrics) TickDone(time.Duration) {}
func (NopMetrics) TicksSkipped(int)       {}
func (NopMetrics) TPS(float64)            {}
func (NopMetrics) Players(Kind, int)      {}
func (NopMetrics) Disconnect(string)      {}
func (NopMetrics) JoinRejected(string)    {}
func (NopMetrics) RejectedInput(string)   {}
func (NopMetrics) ChunksSent(int)         {}
func (NopMetrics) InboxLen(int)           {}

// Options — зависимости мира.
type Options struct {
	Log     *slog.Logger
	Metrics Metrics
	// Now — часы. В тестах — фейковые: мир читает время только для
	// измерения длительности тика.
	Now func() time.Time
	// Snapshots получает снапшоты мира (асинхронное сохранение). Busy —
	// прошлый снапшот ещё не записан (тогда тик не делает копию); Offer не
	// должен блокировать.
	Snapshots SnapshotSink
}

// SnapshotSink — получатель снапшотов (internal/save.Store).
type SnapshotSink interface {
	Busy() bool           // прошлый снапшот ещё не записан
	Skip()                // тик пропустил снапшот, потому что Busy (для метрики)
	Offer(*Snapshot) bool // отдать снапшот на запись, не блокируясь
}

// Snapshot — сохраняемое состояние мира: правки блоков и позиции игроков.
// Создаётся в горутине тика (копия), пишется на диск в другой горутине.
type Snapshot struct {
	Tick    uint64        `json:"tick"`
	Players []PlayerState `json:"players"`
	Blocks  []BlockEdit   `json:"blocks"`
}

// PlayerState — сохранённая позиция игрока (по имени: в offline-режиме
// имя и есть личность).
type PlayerState struct {
	Name  string  `json:"name"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Z     float64 `json:"z"`
	Yaw   float32 `json:"yaw"`
	Pitch float32 `json:"pitch"`
}

// BlockEdit — блок, отличающийся от генератора.
type BlockEdit struct {
	X     int32 `json:"x"`
	Y     int32 `json:"y"`
	Z     int32 `json:"z"`
	State State `json:"state"`
}

// Sort упорядочивает игроков по имени и правки по координатам, чтобы файл
// снапшота был стабильным (удобно сравнивать). Вызывается вне тика —
// горутиной сохранения.
func (s *Snapshot) Sort() {
	slices.SortFunc(s.Players, func(a, b PlayerState) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(s.Blocks, func(a, b BlockEdit) int {
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Z, b.Z), cmp.Compare(a.Y, b.Y))
	})
}

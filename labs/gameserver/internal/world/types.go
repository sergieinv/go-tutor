// Пакет world — игровой мир: состояние, игровой цикл, проверка ввода,
// interest management и рассылка изменений.
//
// Главное правило: состоянием мира владеет одна горутина — та, что
// крутит тики (Run, а в тестах — прямые вызовы Tick). Никаких мьютексов
// вокруг игроков и блоков: соединения общаются с миром только сообщениями.
// Входящие команды приходят через ограниченный канал (Submit), исходящие
// изменения уходят в ограниченную очередь каждого клиента (Peer).
//
// Мир не знает ни Minecraft, ни WebSocket: он порождает нейтральные
// события (Event), а кодирует их в байты Protocol конкретного клиента.
package world

import "math"

// EntityID — id сущности. Один на всех клиентов: Minecraft-клиент и
// браузер видят одни и те же номера.
type EntityID int32

// Vec3 — позиция в мире (ноги игрока).
type Vec3 struct{ X, Y, Z float64 }

func (v Vec3) finite() bool {
	return !math.IsNaN(v.X) && !math.IsNaN(v.Y) && !math.IsNaN(v.Z) &&
		!math.IsInf(v.X, 0) && !math.IsInf(v.Y, 0) && !math.IsInf(v.Z, 0)
}

// ChunkPos — координаты чанка (столб 16×16 блоков).
type ChunkPos struct{ X, Z int32 }

// ChunkOf — чанк, в котором стоит точка.
func ChunkOf(v Vec3) ChunkPos {
	return ChunkPos{int32(math.Floor(v.X)) >> 4, int32(math.Floor(v.Z)) >> 4}
}

// chebyshev — расстояние между чанками «по-королевски»: квадрат видимости.
func chebyshev(a, b ChunkPos) int32 {
	dx, dz := a.X-b.X, a.Z-b.Z
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	return max(dx, dz)
}

// BlockPos — координаты блока.
type BlockPos struct{ X, Y, Z int32 }

// Kind — каким клиентом подключён игрок.
type Kind uint8

const (
	KindMinecraft Kind = iota + 1
	KindWeb
)

func (k Kind) String() string {
	switch k {
	case KindMinecraft:
		return "minecraft"
	case KindWeb:
		return "web"
	}
	return "unknown"
}

// PlayerInfo — то, что о игроке знают другие клиенты. Заполняется миром;
// кодировщики только читают (в той же горутине тика).
type PlayerInfo struct {
	ID    EntityID
	UUID  [16]byte
	Name  string
	Kind  Kind
	Pos   Vec3
	Yaw   float32
	Pitch float32
}

// Причины отключения — значения метки reason метрики отключений.
// Набор фиксирован: метка с произвольными значениями раздувает метрики.
const (
	ReasonQuit         = "quit"          // клиент вышел сам / соединение закрыто
	ReasonTimeout      = "timeout"       // нет ответа на keep-alive, дедлайн чтения
	ReasonSlowConsumer = "slow_consumer" // очередь исходящих переполнена
	ReasonRateLimit    = "rate_limit"    // слишком много пакетов
	ReasonBadPacket    = "bad_packet"    // не разобрали пакет / нарушение протокола
	ReasonBadInput     = "bad_input"     // NaN/Inf в координатах и т. п.
	ReasonShutdown     = "shutdown"      // сервер останавливается
	ReasonDuplicate    = "duplicate"     // вход с тем же именем
)

// DisconnectReasons — все причины отключения (для метрик).
var DisconnectReasons = []string{ReasonQuit, ReasonTimeout, ReasonSlowConsumer,
	ReasonRateLimit, ReasonBadPacket, ReasonBadInput, ReasonShutdown}

// JoinRejectReasons — причины отказа во входе.
var JoinRejectReasons = []string{"full", ReasonDuplicate}

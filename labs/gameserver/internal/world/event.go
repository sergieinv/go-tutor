package world

// EventKind — тип исходящего события.
type EventKind uint8

const (
	// EvWelcome — первое событие нового игрока: его id, позиция, параметры
	// мира. Minecraft: Login (play), способности, граница мира, позиция,
	// «start waiting for level chunks». Браузер: welcome.
	EvWelcome EventKind = iota + 1
	// EvRosterAdd / EvRosterRemove — игрок вошёл / вышел (список игроков,
	// Tab). Рассылается всем, независимо от видимости.
	EvRosterAdd
	EvRosterRemove
	// EvSpawn / EvDespawn — сущность игрока появилась / пропала из зоны
	// видимости получателя.
	EvSpawn
	EvDespawn
	// EvMove — перемещение и/или поворот видимой сущности.
	EvMove
	// EvChat — сообщение игрока, EvSystem — системное сообщение.
	EvChat
	EvSystem
	// Чанки: начало и конец пачки (Minecraft: Chunk Batch Start/Finished),
	// загрузка, выгрузка, новый центр зоны видимости.
	EvChunkBatchBegin
	EvChunk
	EvChunkBatchEnd
	EvUnchunk
	EvCenter
	// EvBlock — блок изменился; EvBlockAck — подтверждение действия
	// игрока с блоком (Minecraft ждёт его, чтобы снять предсказание).
	EvBlock
	EvBlockAck
	// EvTeleport — сервер поправляет позицию игрока (ввод отклонён).
	EvTeleport
	// EvKick — последнее сообщение перед отключением.
	EvKick
)

// Event — событие для кодировщика протокола. Поля заполнены в
// зависимости от Kind. Событие живёт только в пределах тика: кодировщик
// не должен сохранять ссылки на него.
type Event struct {
	Kind EventKind
	// Player — субъект события: кто вошёл, появился, двигается, пишет в чат.
	Player *PlayerInfo
	// IDs — для EvDespawn.
	IDs []EntityID

	// EvMove: прошлая разосланная и новая позиция.
	From, To   Vec3
	Yaw, Pitch float32
	Moved      bool // позиция изменилась
	Rotated    bool // поворот изменился
	OnGround   bool

	Text   string // чат, системное сообщение, причина отключения
	Reason string // EvTeleport: почему поправили (too_fast, out_of_bounds…)

	Chunk  ChunkPos
	Column *Column // EvChunk: правки чанка (nil — чанк не меняли)
	Count  int     // EvChunkBatchEnd: сколько чанков в пачке

	Block    BlockPos
	State    State
	TopY     int32 // EvBlock: новый верх колонки (для карты сверху)
	TopState State
	Seq      int32 // EvBlockAck: номер действия клиента

	TeleportID int32 // EvWelcome, EvTeleport

	Welcome *Welcome
}

// Welcome — параметры мира для нового игрока.
type Welcome struct {
	ViewDistance int
	MaxPlayers   int
	BorderRadius float64
	Spawn        Vec3
	TickRate     int
	Placeable    []string // какие блоки браузерный клиент может ставить
	MaxChat      int
}

// BatchStats — статистика тика для кодировщика: браузерный клиент
// показывает её в визуализаторе.
type BatchStats struct {
	Tick   uint64
	MSPT   float64 // длительность прошлого тика, мс
	TPS    float64
	Events int // сколько событий ушло этому клиенту за тик
}

// Protocol превращает события в байты своего протокола. Все методы
// вызываются только из горутины тика, поэтому кодировщик может держать
// внутри кеши и буферы без блокировок. Один экземпляр Protocol обслуживает
// всех клиентов своего типа: широковещательное событие кодируется один
// раз и копируется в очереди всех получателей.
type Protocol interface {
	// Begin начинает пачку событий одного тика для одного клиента.
	Begin(dst []byte) []byte
	// Append дописывает событие. Может не дописать ничего, если событие
	// протоколу не нужно.
	Append(dst []byte, ev *Event) []byte
	// End завершает пачку. Если в пачке нет событий и протоколу нечего
	// добавить, возвращает dst без изменений — тогда пачка не отправляется.
	End(dst []byte, st *BatchStats) []byte
}

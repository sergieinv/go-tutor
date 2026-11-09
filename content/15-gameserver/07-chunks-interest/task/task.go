package task

// ChunkPos — координаты чанка 16×16 блоков.
type ChunkPos struct{ X, Z int }

// PlayerID — идентификатор игрока.
type PlayerID int

// MaxViewDistance — наибольшая дальность видимости в чанках.
const MaxViewDistance = 32

// ChunkOf возвращает чанк, в котором лежит точка (x, z).
func ChunkOf(x, z float64) ChunkPos {
	// TODO: округление вниз, а не к нулю
	return ChunkPos{}
}

// BlockChunk возвращает чанк блока с целыми координатами (bx, bz).
func BlockChunk(bx, bz int) ChunkPos {
	// TODO
	return ChunkPos{}
}

// Interest хранит зоны видимости игроков и подписчиков каждого чанка.
type Interest struct {
	// TODO: игроки (центр зоны и дальность) и обратный индекс «чанк → подписчики»
}

// NewInterest возвращает пустой Interest.
func NewInterest() *Interest {
	return &Interest{}
}

// Join добавляет игрока и возвращает чанки, которые ему нужно загрузить.
func (in *Interest) Join(id PlayerID, x, z float64, r int) []ChunkPos {
	// TODO
	return nil
}

// Move перемещает игрока и возвращает разность зон видимости.
func (in *Interest) Move(id PlayerID, x, z float64) (load, unload []ChunkPos) {
	// TODO
	return nil, nil
}

// SetViewDistance меняет дальность видимости игрока.
func (in *Interest) SetViewDistance(id PlayerID, r int) (load, unload []ChunkPos) {
	// TODO
	return nil, nil
}

// Leave удаляет игрока и возвращает чанки его зоны.
func (in *Interest) Leave(id PlayerID) []ChunkPos {
	// TODO
	return nil
}

// Subscribers возвращает игроков, в зоне которых лежит чанк, по возрастанию id.
func (in *Interest) Subscribers(c ChunkPos) []PlayerID {
	// TODO
	return nil
}

// Viewers возвращает игроков, которые видят игрока id (кроме него самого).
func (in *Interest) Viewers(id PlayerID) []PlayerID {
	// TODO
	return nil
}

// BlockChanged возвращает получателей события «блок (bx, bz) изменён».
func (in *Interest) BlockChanged(bx, bz int) []PlayerID {
	// TODO
	return nil
}

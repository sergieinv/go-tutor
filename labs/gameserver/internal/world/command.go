package world

import (
	"context"
	"errors"
)

// CommandKind — тип входящей команды.
type CommandKind uint8

const (
	CmdJoin        CommandKind = iota + 1 // вход (Join)
	CmdLeave                              // выход (Reason)
	CmdMove                               // перемещение (Pos/Yaw/Pitch, HasPos/HasRot)
	CmdTeleportAck                        // клиент принял поправку позиции (TeleportID)
	CmdChat                               // сообщение в чат (Text)
	CmdDig                                // сломать блок (Block, Seq)
	CmdPlace                              // поставить блок (Block, State, Seq)
	CmdKick                               // соединение требует отключить игрока (Reason)
)

// Command — ввод от соединения. Значение, а не интерфейс: отправка в
// канал не аллоцирует.
type Command struct {
	Kind   CommandKind
	Player EntityID

	Pos      Vec3
	Yaw      float32
	Pitch    float32
	OnGround bool
	HasPos   bool
	HasRot   bool

	TeleportID int32
	Text       string
	Block      BlockPos
	State      State
	Seq        int32
	Reason     string

	Join *JoinRequest
}

// JoinRequest — запрос на вход. Ответ приходит в Reply (буфер на 1).
type JoinRequest struct {
	Name  string
	UUID  [16]byte
	Peer  *Peer
	Reply chan JoinResult
}

// JoinResult — ответ мира на вход.
type JoinResult struct {
	ID  EntityID
	Err error // ErrFull, ErrDuplicate, ErrShuttingDown
}

var (
	ErrFull         = errors.New("server is full")
	ErrDuplicate    = errors.New("player with this name is already online")
	ErrShuttingDown = errors.New("server is shutting down")
)

// Submit отправляет команду в мир. Канал ограничен: если мир не успевает,
// горутина чтения соединения ждёт — это естественное обратное давление,
// клиент упирается в TCP-окно. Ждём не дольше, чем живёт ctx.
func (w *World) Submit(ctx context.Context, c Command) bool {
	select {
	case w.inbox <- c:
		return true
	case <-ctx.Done():
		return false
	case <-w.stopped:
		return false
	}
}

// Join — вход игрока: отправляет запрос и ждёт ответа мира.
func (w *World) Join(ctx context.Context, name string, uuid [16]byte, peer *Peer) (EntityID, error) {
	req := &JoinRequest{Name: name, UUID: uuid, Peer: peer, Reply: make(chan JoinResult, 1)}
	if !w.Submit(ctx, Command{Kind: CmdJoin, Join: req}) {
		return 0, ErrShuttingDown
	}
	select {
	case r := <-req.Reply:
		return r.ID, r.Err
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-w.stopped:
		return 0, ErrShuttingDown
	}
}

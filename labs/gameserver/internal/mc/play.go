package mc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/Tnze/go-mc/data/registryid"

	"gotutor/labs/gameserver/internal/limit"
	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/world"
)

// keepAlive — состояние keep-alive, общее для горутин чтения и записи.
type keepAlive struct {
	id      atomic.Int64 // id последнего отправленного
	sentAt  atomic.Int64 // когда отправлен (UnixNano)
	pending atomic.Bool  // ответ ещё не пришёл
}

// play — фаза игры. Горутина чтения — текущая, горутина записи —
// отдельная. Чтение разбирает пакеты в команды мира, запись отправляет
// пачки тиков из очереди Peer и keep-alive.
func (ss *session) play(ctx context.Context, eid world.EntityID, peer *world.Peer, log *slog.Logger) {
	s := ss.s
	ka := &keepAlive{}
	done := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		ss.writeLoop(peer, ka, done, log)
	}()

	reason := ss.readLoop(ctx, eid, peer, ka)
	// Мир уже мог отключить игрока сам (медленный клиент, NaN) — тогда
	// команда выхода ничего не сделает.
	s.world.Submit(context.WithoutCancel(ctx), world.Command{Kind: world.CmdLeave, Player: eid, Reason: reason})
	close(done)
	ss.conn.Close()
	<-writerDone
	if r := peer.Reason(); r != "" {
		reason = r
	}
	log.Info("minecraft connection closed", "id", eid, "reason", reason)
}

// readLoop читает пакеты до ошибки и возвращает причину отключения.
func (ss *session) readLoop(ctx context.Context, eid world.EntityID, peer *world.Peer, ka *keepAlive) string {
	s := ss.s
	rate := limit.New(s.cfg.PacketsPerSecond, s.cfg.PacketBurst, time.Now())
	in := newInput(ka)
	for {
		_ = ss.conn.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
		id, payload, err := ss.fr.Next()
		if err != nil {
			return readErrReason(err)
		}
		s.metrics.PacketIn(protoName)
		if !rate.Allow(time.Now()) {
			return world.ReasonRateLimit
		}
		cmd, err := in.decode(id, payload)
		if err != nil {
			return world.ReasonBadPacket
		}
		if cmd.Kind != 0 {
			cmd.Player = eid
			if !s.world.Submit(ctx, cmd) {
				return world.ReasonShutdown
			}
		}
		select {
		case <-peer.Closed():
			return peer.Reason()
		default:
		}
	}
}

// input — состояние разбора входящих пакетов одного соединения.
type input struct {
	ka       *keepAlive
	hotbar   [9]int32 // предметы панели быстрого доступа (креатив)
	offhand  int32
	selected int
	items    map[int32]world.State // кеш «предмет → блок»
}

func newInput(ka *keepAlive) *input { return &input{ka: ka, items: map[int32]world.State{}} }

// decode превращает пакет в команду мира (Kind == 0 — команды нет).
// Чистая функция от байт и состояния соединения: её проверяют тесты и fuzz.
func (in *input) decode(id int32, payload []byte) (world.Command, error) {
	d := mcproto.NewDecoder(payload)
	var cmd world.Command
	// exact — формат пакета фиксирован, лишние байты — ошибка (признак
	// чужой версии протокола или подделки). У чата (подпись, подтверждения)
	// и предмета в слоте (компоненты) хвост переменный — его пропускаем.
	exact := true
	switch id {
	case sbAcceptTeleportation:
		cmd = world.Command{Kind: world.CmdTeleportAck, TeleportID: d.VarInt()}
	case sbKeepAlive:
		if v := d.Long(); d.Err() == nil && v == in.ka.id.Load() {
			in.ka.pending.Store(false)
		}
	case sbMovePlayerPos:
		cmd = world.Command{Kind: world.CmdMove, HasPos: true}
		cmd.Pos = world.Vec3{X: d.Double(), Y: d.Double(), Z: d.Double()}
		cmd.OnGround = d.Bool()
	case sbMovePlayerPosRot:
		cmd = world.Command{Kind: world.CmdMove, HasPos: true, HasRot: true}
		cmd.Pos = world.Vec3{X: d.Double(), Y: d.Double(), Z: d.Double()}
		cmd.Yaw, cmd.Pitch = d.Float(), d.Float()
		cmd.OnGround = d.Bool()
	case sbMovePlayerRot:
		cmd = world.Command{Kind: world.CmdMove, HasRot: true}
		cmd.Yaw, cmd.Pitch = d.Float(), d.Float()
		cmd.OnGround = d.Bool()
	case sbMovePlayerStatus:
		_ = d.Bool()
	case sbChat:
		// Сообщение, время, соль, подпись, подтверждения — нам нужен
		// только текст (в offline подписей нет).
		cmd = world.Command{Kind: world.CmdChat, Text: d.String(256)}
		_ = d.Rest()
		exact = false
	case sbPlayerAction:
		status := d.VarInt()
		x, y, z := d.Position()
		_ = d.Byte() // грань
		seq := d.VarInt()
		if status == 0 { // начал копать: в креативе блок ломается сразу
			cmd = world.Command{Kind: world.CmdDig, Block: world.BlockPos{X: x, Y: y, Z: z}, Seq: seq}
		}
	case sbSetCarriedItem:
		if slot := int(d.Short()); slot >= 0 && slot < 9 {
			in.selected = slot
		}
	case sbSetCreativeSlot:
		slot := d.Short()
		item := int32(0)
		if count := d.VarInt(); count > 0 {
			item = d.VarInt()
		}
		_ = d.Rest() // компоненты предмета не разбираем
		exact = false
		switch {
		case slot >= 36 && slot <= 44:
			in.hotbar[slot-36] = item
		case slot == 45:
			in.offhand = item
		}
	case sbUseItemOn:
		hand := d.VarInt()
		x, y, z := d.Position()
		face := d.VarInt()
		_, _, _ = d.Float(), d.Float(), d.Float()
		_ = d.Bool()
		seq := d.VarInt()
		item := in.hotbar[in.selected]
		if hand == 1 {
			item = in.offhand
		}
		dx, dy, dz := faceOffset(face)
		cmd = world.Command{
			Kind:  world.CmdPlace,
			Block: world.BlockPos{X: x + dx, Y: y + dy, Z: z + dz},
			State: itemBlock(in.items, item), Seq: seq,
		}
	default:
		// Остальное (настройки клиента, взмах рукой, chunk batch received,
		// команды…) не нужно — пропускаем, но лимит частоты оно расходует.
		return world.Command{}, nil
	}
	err := d.Err()
	if exact {
		err = d.Finish()
	}
	if err != nil {
		return world.Command{}, err
	}
	return cmd, nil
}

// faceOffset — смещение к соседнему блоку по грани (0 −Y, 1 +Y, 2 −Z,
// 3 +Z, 4 −X, 5 +X).
func faceOffset(face int32) (int32, int32, int32) {
	switch face {
	case 0:
		return 0, -1, 0
	case 1:
		return 0, 1, 0
	case 2:
		return 0, 0, -1
	case 3:
		return 0, 0, 1
	case 4:
		return -1, 0, 0
	case 5:
		return 1, 0, 0
	}
	return 0, 0, 0
}

// itemBlock — блок, который ставит предмет: у блочных предметов имя
// предмета совпадает с именем блока (minecraft:stone). 0 (воздух) — не
// блок; мир такой запрос отклонит.
func itemBlock(cache map[int32]world.State, item int32) world.State {
	if s, ok := cache[item]; ok {
		return s
	}
	var s world.State
	if item > 0 && int(item) < len(registryid.Item) {
		s, _ = world.DefaultState(registryid.Item[item])
	}
	if len(cache) < 1024 {
		cache[item] = s
	}
	return s
}

func readErrReason(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed), errors.Is(err, io.ErrUnexpectedEOF):
		return world.ReasonQuit
	case errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return world.ReasonTimeout
	case errors.Is(err, mcproto.ErrFrameTooBig), errors.Is(err, mcproto.ErrBadCompressed),
		errors.Is(err, mcproto.ErrVarIntTooBig), errors.Is(err, mcproto.ErrEmptyFrame):
		return world.ReasonBadPacket
	}
	return world.ReasonQuit
}

// writeLoop отправляет пачки тиков и keep-alive. Пишет в сокет только
// она — поэтому без мьютекса.
func (ss *session) writeLoop(peer *world.Peer, ka *keepAlive, done <-chan struct{}, log *slog.Logger) {
	s := ss.s
	pw := mcproto.NewPacketWriter(s.enc.Threshold())
	var kaBuf []byte
	ticker := time.NewTicker(s.cfg.KeepAliveInterval)
	defer ticker.Stop()

	write := func(b []byte) bool {
		_ = ss.conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
		if _, err := ss.conn.Write(b); err != nil {
			return false
		}
		s.metrics.BytesOut(protoName, len(b))
		return true
	}
	for {
		select {
		case b := <-peer.Out():
			ok := write(b.B)
			world.PutBuffer(b)
			if !ok {
				ss.conn.Close()
				return
			}
		case now := <-ticker.C:
			// Ответа на прошлый keep-alive нет 3 периода — клиент пропал.
			if ka.pending.Load() && now.Sub(time.Unix(0, ka.sentAt.Load())) > 3*s.cfg.KeepAliveInterval {
				log.Info("keep-alive timeout")
				peer.Close(world.ReasonTimeout)
				ss.conn.Close()
				return
			}
			if !ka.pending.Load() {
				id := now.UnixMilli()
				ka.id.Store(id)
				ka.sentAt.Store(now.UnixNano())
				ka.pending.Store(true)
				b := pw.Begin(cbKeepAlive)
				b = mcproto.AppendLong(b, id)
				kaBuf = pw.Frame(kaBuf[:0], b)
				if !write(kaBuf) {
					ss.conn.Close()
					return
				}
			}
		case <-peer.Closed():
			// Мир отключил игрока: дописываем то, что уже в очереди (там
			// пакет Disconnect с причиной), коротким дедлайном — и закрываем.
			deadline := time.Now().Add(time.Second)
			for {
				select {
				case b := <-peer.Out():
					_ = ss.conn.SetWriteDeadline(deadline)
					_, err := ss.conn.Write(b.B)
					world.PutBuffer(b)
					if err != nil {
						ss.conn.Close()
						return
					}
				default:
					ss.conn.Close()
					return
				}
			}
		case <-done:
			return
		}
	}
}

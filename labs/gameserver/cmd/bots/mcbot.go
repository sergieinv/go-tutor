package main

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/Tnze/go-mc/data/packetid"

	"gotutor/labs/gameserver/internal/mcproto"
)

// Номера пакетов протокола 767 — из go-mc, как и в сервере.
var (
	sbLoginStart   = int32(packetid.ServerboundLoginHello)
	sbLoginAck     = int32(packetid.ServerboundLoginLoginAcknowledged)
	sbKnownPacks   = int32(packetid.ServerboundConfigSelectKnownPacks)
	sbConfigFinish = int32(packetid.ServerboundConfigFinishConfiguration)
	sbConfigKA     = int32(packetid.ServerboundConfigKeepAlive)
	sbTeleportAck  = int32(packetid.ServerboundAcceptTeleportation)
	sbKeepAlive    = int32(packetid.ServerboundKeepAlive)
	sbMovePosRot   = int32(packetid.ServerboundMovePlayerPosRot)
	sbChat         = int32(packetid.ServerboundChat)

	cbLoginDisconnect = int32(packetid.ClientboundLoginLoginDisconnect)
	cbLoginSuccess    = int32(packetid.ClientboundLoginGameProfile)
	cbCompression     = int32(packetid.ClientboundLoginLoginCompression)
	cbConfigDisc      = int32(packetid.ClientboundConfigDisconnect)
	cbConfigFinish    = int32(packetid.ClientboundConfigFinishConfiguration)
	cbConfigKA        = int32(packetid.ClientboundConfigKeepAlive)
	cbKnownPacks      = int32(packetid.ClientboundConfigSelectKnownPacks)
	cbDisconnect      = int32(packetid.ClientboundDisconnect)
	cbKeepAlive       = int32(packetid.ClientboundKeepAlive)
	cbPlayerPosition  = int32(packetid.ClientboundPlayerPosition)
	cbSystemChat      = int32(packetid.ClientboundSystemChat)
)

// mcBot — лёгкий клиент Minecraft на mcproto: вход, configuration,
// ответы на keep-alive и телепорты, ходьба и чат. Сжатые пакеты (чанки)
// не распаковывает — боту они не нужны, а CPU ноутбука делится с сервером.
type mcBot struct {
	name  string
	cfg   *botConfig
	stats *stats

	conn net.Conn
	fr   *mcproto.FrameReader
	wmu  sync.Mutex
	pw   *mcproto.PacketWriter
	wbuf []byte

	mu       sync.Mutex
	pos      [3]float64
	tpID     int32
	havePos  bool
	tpQueued bool
}

func (b *mcBot) write(body []byte) error {
	b.wbuf = b.pw.Frame(b.wbuf[:0], body)
	_ = b.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := b.conn.Write(b.wbuf)
	return err
}

// send собирает и пишет пакет под мьютексом (пишут две горутины).
func (b *mcBot) send(id int32, fill func([]byte) []byte) error {
	b.wmu.Lock()
	defer b.wmu.Unlock()
	body := b.pw.Begin(id)
	if fill != nil {
		body = fill(body)
	}
	return b.write(body)
}

func (b *mcBot) run(ctx context.Context) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", b.cfg.mcAddr)
	if err != nil {
		return joinError{err}
	}
	b.conn = conn
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	b.fr = mcproto.NewFrameReader(bufio.NewReaderSize(conn, 64<<10), mcproto.MaxFrameLen)
	b.fr.MaxData = mcproto.MaxFrameLen
	b.fr.SkipCompressed = true
	b.pw = mcproto.NewPacketWriter(-1)

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := b.login(); err != nil {
		return joinError{err}
	}
	_ = conn.SetDeadline(time.Time{})
	b.stats.joined.Add(1)

	walkerDone := make(chan struct{})
	go func() {
		defer close(walkerDone)
		b.walk(ctx)
	}()
	err = b.readLoop()
	conn.Close()
	<-walkerDone
	return err
}

func (b *mcBot) login() error {
	host, port := splitHostPort(b.cfg.mcAddr)
	if err := b.send(0x00, func(p []byte) []byte {
		p = mcproto.AppendVarInt(p, 767)
		p = mcproto.AppendString(p, host)
		p = mcproto.AppendShort(p, int16(port))
		return mcproto.AppendVarInt(p, 2)
	}); err != nil {
		return err
	}
	if err := b.send(sbLoginStart, func(p []byte) []byte {
		p = mcproto.AppendString(p, b.name)
		return mcproto.AppendUUID(p, mcproto.OfflineUUID(b.name))
	}); err != nil {
		return err
	}
	for {
		id, p, err := b.fr.Next()
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		switch id {
		case cbCompression:
			d := mcproto.NewDecoder(p)
			t := int(d.VarInt())
			b.fr.SetCompression(t)
			b.pw.SetThreshold(t)
		case cbLoginDisconnect:
			return fmt.Errorf("login refused: %s", p)
		case cbLoginSuccess:
			if err := b.send(sbLoginAck, nil); err != nil {
				return err
			}
			return b.configure()
		}
	}
}

func (b *mcBot) configure() error {
	for {
		id, p, err := b.fr.Next()
		if err != nil {
			return fmt.Errorf("configuration: %w", err)
		}
		switch id {
		case cbKnownPacks: // «знаю все предложенные наборы»
			if err := b.send(sbKnownPacks, func(dst []byte) []byte { return append(dst, p...) }); err != nil {
				return err
			}
		case cbConfigKA:
			if err := b.send(sbConfigKA, func(dst []byte) []byte { return append(dst, p...) }); err != nil {
				return err
			}
		case cbConfigDisc:
			return fmt.Errorf("refused in configuration")
		case cbConfigFinish:
			return b.send(sbConfigFinish, nil)
		}
	}
}

// readLoop — ответы серверу. Возвращает ошибку при отключении.
func (b *mcBot) readLoop() error {
	for {
		_ = b.conn.SetReadDeadline(time.Now().Add(40 * time.Second))
		id, p, err := b.fr.Next()
		if err != nil {
			return err
		}
		b.stats.packetsIn.Add(1)
		switch id {
		case cbKeepAlive:
			if err := b.send(sbKeepAlive, func(dst []byte) []byte { return append(dst, p...) }); err != nil {
				return err
			}
		case cbPlayerPosition:
			d := mcproto.NewDecoder(p)
			x, y, z := d.Double(), d.Double(), d.Double()
			_, _, _ = d.Float(), d.Float(), d.Byte()
			tp := d.VarInt()
			if d.Err() != nil {
				return d.Err()
			}
			b.mu.Lock()
			if b.havePos {
				b.stats.corrections.Add(1)
			}
			b.pos, b.havePos, b.tpID = [3]float64{x, y, z}, true, tp
			b.mu.Unlock()
			if err := b.send(sbTeleportAck, func(dst []byte) []byte { return mcproto.AppendVarInt(dst, tp) }); err != nil {
				return err
			}
		case cbSystemChat:
			b.stats.chatIn.Add(1)
		case cbDisconnect:
			return fmt.Errorf("kicked: %q", p)
		}
	}
}

// walk — каждые 50 мс шаг к случайной цели в квадрате spread вокруг
// точки появления; изредка — сообщение в чат.
func (b *mcBot) walk(ctx context.Context) {
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(len(b.name))))
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	var tx, tz float64
	newTarget := func() {
		tx = (rng.Float64()*2 - 1) * b.cfg.spread
		tz = (rng.Float64()*2 - 1) * b.cfg.spread
	}
	newTarget()
	nextChat := time.Now().Add(time.Duration(rng.Int64N(int64(2 * b.cfg.chatEvery))))
	step := b.cfg.speed / 20
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			b.mu.Lock()
			if !b.havePos {
				b.mu.Unlock()
				continue
			}
			dx, dz := tx-b.pos[0], tz-b.pos[2]
			dist := math.Hypot(dx, dz)
			if dist < step {
				newTarget()
				b.mu.Unlock()
				continue
			}
			b.pos[0] += dx / dist * step
			b.pos[2] += dz / dist * step
			x, y, z := b.pos[0], b.pos[1], b.pos[2]
			b.mu.Unlock()
			yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
			if err := b.send(sbMovePosRot, func(p []byte) []byte {
				p = mcproto.AppendDouble(p, x)
				p = mcproto.AppendDouble(p, y)
				p = mcproto.AppendDouble(p, z)
				p = mcproto.AppendFloat(p, yaw)
				p = mcproto.AppendFloat(p, 0)
				return mcproto.AppendBool(p, true)
			}); err != nil {
				return
			}
			b.stats.moves.Add(1)
			if b.cfg.chatEvery > 0 && now.After(nextChat) {
				nextChat = now.Add(time.Duration(rng.Int64N(int64(2 * b.cfg.chatEvery))))
				msg := fmt.Sprintf("hello from %s", b.name)
				if err := b.send(sbChat, func(p []byte) []byte {
					p = mcproto.AppendString(p, msg)
					p = mcproto.AppendLong(p, now.UnixMilli())
					p = mcproto.AppendLong(p, 0)     // соль
					p = mcproto.AppendBool(p, false) // без подписи
					p = mcproto.AppendVarInt(p, 0)   // message count
					return append(p, 0, 0, 0)        // acknowledged: BitSet на 20 бит
				}); err != nil {
					return
				}
				b.stats.chatOut.Add(1)
			}
		}
	}
}

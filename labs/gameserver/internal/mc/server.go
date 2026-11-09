package mc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/world"
)

// Config — параметры Minecraft-сервера.
type Config struct {
	MaxConns             int           // одновременных TCP-соединений (включая пинги и вход)
	CompressionThreshold int           // порог сжатия, байт; -1 — без сжатия
	LoginTimeout         time.Duration // на handshake + login + configuration
	ReadTimeout          time.Duration // тишина от клиента в игре
	WriteTimeout         time.Duration // запись одной пачки
	KeepAliveInterval    time.Duration // период keep-alive (клиент ждёт не дольше 15–20 с)
	QueueLen             int           // очередь исходящих пачек (тиков)
	MaxPacket            int           // предел входящего пакета в игре, байт
	PacketsPerSecond     float64       // лимит частоты входящих пакетов…
	PacketBurst          float64       // …и запас
	MOTD                 string        // описание в списке серверов
}

// DefaultConfig — настройки по умолчанию.
func DefaultConfig() Config {
	return Config{
		MaxConns:             512,
		CompressionThreshold: 256,
		LoginTimeout:         10 * time.Second,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         10 * time.Second,
		KeepAliveInterval:    10 * time.Second,
		QueueLen:             64,
		MaxPacket:            32 << 10,
		PacketsPerSecond:     100,
		PacketBurst:          300,
		MOTD:                 "go-tutor labs — учебный сервер (неофициальный)",
	}
}

// ConnMetrics — метрики соединений (общие для Minecraft и WebSocket).
type ConnMetrics interface {
	ConnOpened(proto string)
	ConnClosed(proto string)
	ConnRejected(proto, reason string)
	BytesOut(proto string, n int)
	PacketIn(proto string)
}

const protoName = "minecraft"

// Server принимает соединения Minecraft.
type Server struct {
	cfg     Config
	log     *slog.Logger
	world   *world.World
	enc     *Encoder
	metrics ConnMetrics

	registryPackets [][]byte
	sem             chan struct{}
	wg              sync.WaitGroup
}

// NewServer создаёт сервер. enc — общий кодировщик всех Minecraft-клиентов:
// его порог сжатия объявляется клиентам в Set Compression.
func NewServer(cfg Config, w *world.World, enc *Encoder, m ConnMetrics, log *slog.Logger) *Server {
	return &Server{
		cfg: cfg, log: log, world: w, enc: enc, metrics: m,
		registryPackets: registryPackets(),
		sem:             make(chan struct{}, cfg.MaxConns),
	}
}

// Serve принимает соединения, пока не отменён ctx. Каждое соединение —
// в своей горутине; Wait дожидается их завершения.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	defer stop()
	var backoff time.Duration
	var lastLog time.Time
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// Временная ошибка (EMFILE «слишком много файлов», ECONNABORTED…)
			// не должна останавливать сервер: пауза с удвоением 5 мс → 1 с,
			// как в net/http. Лог — не чаще раза в секунду.
			backoff = min(max(2*backoff, 5*time.Millisecond), time.Second)
			if time.Since(lastLog) > time.Second {
				s.log.Warn("accept failed, retrying", "err", err, "delay", backoff)
				lastLog = time.Now()
			}
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return nil
			}
		}
		backoff = 0
		select {
		case s.sem <- struct{}{}:
		default:
			// Лимит соединений: закрываем сразу, не читая ни байта.
			s.metrics.ConnRejected(protoName, "conn_limit")
			conn.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.sem }()
			s.metrics.ConnOpened(protoName)
			defer s.metrics.ConnClosed(protoName)
			s.handle(ctx, conn)
		}()
	}
}

// Wait ждёт завершения всех соединений (не дольше ctx).
func (s *Server) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// session — одно соединение до входа в игру.
type session struct {
	s    *Server
	conn net.Conn
	fr   *mcproto.FrameReader
	pw   *mcproto.PacketWriter
	out  []byte
	log  *slog.Logger
	// stopClose снимает закрытие соединения по остановке сервера: в игре
	// закрытием управляет мир (kick всем игрокам и дренаж очереди).
	stopClose func() bool
	packets   int // пакетов до входа в игру
}

func (ss *session) send(body []byte) error {
	ss.out = ss.pw.Frame(ss.out[:0], body)
	_, err := ss.conn.Write(ss.out)
	if err == nil {
		ss.s.metrics.BytesOut(protoName, len(ss.out))
	}
	return err
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	// До входа в игру остановка сервера закрывает соединение сразу: kick
	// таким клиентам слать некуда (мир их ещё не знает), а ждать их
	// LoginTimeout — значит съесть время на дренаж игроков.
	stopClose := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopClose()
	// Дедлайн на весь вход: медленный (или злонамеренный) клиент не
	// займёт слот соединения навсегда.
	_ = conn.SetDeadline(time.Now().Add(s.cfg.LoginTimeout))
	ss := &session{
		s: s, conn: conn, stopClose: stopClose,
		fr:  mcproto.NewFrameReader(bufio.NewReaderSize(conn, 4096), 1024),
		pw:  mcproto.NewPacketWriter(-1),
		log: s.log.With("remote", conn.RemoteAddr().String()),
	}

	// Handshake: версия протокола, адрес, порт, следующее состояние.
	id, payload, err := ss.next()
	if err != nil || id != sbHandshake {
		return
	}
	d := mcproto.NewDecoder(payload)
	proto := d.VarInt()
	_ = d.String(255) // адрес
	_ = d.UShort()    // порт
	next := d.VarInt()
	if d.Finish() != nil {
		return
	}
	switch next {
	case 1:
		ss.status()
	case 2:
		ss.login(ctx, proto)
	default: // 3 — transfer, не поддерживаем
	}
}

// maxPrePlayPackets — сколько пакетов клиент может прислать до входа в
// игру. Честному клиенту хватает десятка; без предела соединение могло бы
// весь LoginTimeout слать пакеты, которые фаза configuration пропускает.
const maxPrePlayPackets = 128

var errTooManyPackets = errors.New("too many packets before play")

// next — следующий пакет до входа в игру, с пределом числа пакетов.
func (ss *session) next() (int32, []byte, error) {
	ss.packets++
	if ss.packets > maxPrePlayPackets {
		return 0, nil, errTooManyPackets
	}
	return ss.fr.Next()
}

// status — пинг списка серверов: JSON с версией и онлайном, затем ping/pong.
func (ss *session) status() {
	id, _, err := ss.next()
	if err != nil || id != sbStatusRequest {
		return
	}
	resp := map[string]any{
		"version":            map[string]any{"name": VersionName, "protocol": ProtocolVersion},
		"players":            map[string]any{"max": ss.s.world.MaxPlayers(), "online": ss.s.world.Online()},
		"description":        map[string]any{"text": ss.s.cfg.MOTD},
		"enforcesSecureChat": false,
	}
	js, _ := json.Marshal(resp)
	b := ss.pw.Begin(cbStatusResponse)
	b = mcproto.AppendString(b, string(js))
	if ss.send(b) != nil {
		return
	}
	id, payload, err := ss.next()
	if err != nil || id != sbStatusPing || len(payload) != 8 {
		return
	}
	b = ss.pw.Begin(cbStatusPong)
	b = append(b, payload...)
	_ = ss.send(b)
}

func (ss *session) loginDisconnect(text string) {
	js, _ := json.Marshal(map[string]string{"text": text})
	b := ss.pw.Begin(cbLoginDisconnect)
	b = mcproto.AppendString(b, string(js))
	_ = ss.send(b)
}

func (ss *session) configDisconnect(text string) {
	b := ss.pw.Begin(cbConfigDisconnect)
	b = mcproto.AppendTextPlain(b, text)
	_ = ss.send(b)
}

// login — вход в offline-режиме, configuration, вход в мир и игра.
func (ss *session) login(ctx context.Context, proto int32) {
	s := ss.s
	id, payload, err := ss.next()
	if err != nil || id != sbLoginStart {
		return
	}
	d := mcproto.NewDecoder(payload)
	name := d.String(16)
	_ = d.UUID() // клиент присылает свой UUID; в offline сервер вычисляет его сам
	if d.Finish() != nil {
		return
	}
	if proto != ProtocolVersion {
		ss.loginDisconnect(fmt.Sprintf("Нужен Minecraft %s (протокол %d)", VersionName, ProtocolVersion))
		s.metrics.ConnRejected(protoName, "version")
		return
	}
	if !world.ValidName(name) {
		ss.loginDisconnect("Недопустимое имя: 3–16 символов A-Z, a-z, 0-9, _")
		s.metrics.ConnRejected(protoName, "bad_name")
		return
	}
	uuid := mcproto.OfflineUUID(name)
	log := ss.log.With("name", name)

	// Сжатие включается сразу после Set Compression — в обе стороны.
	if t := s.enc.Threshold(); t >= 0 {
		b := ss.pw.Begin(cbLoginCompression)
		b = mcproto.AppendVarInt(b, int32(t))
		if ss.send(b) != nil {
			return
		}
		ss.pw.SetThreshold(t)
		ss.fr.SetCompression(t)
	}
	b := ss.pw.Begin(cbLoginSuccess)
	b = mcproto.AppendUUID(b, uuid)
	b = mcproto.AppendString(b, name)
	b = mcproto.AppendVarInt(b, 0)   // свойства профиля (скин) — нет
	b = mcproto.AppendBool(b, false) // strict error handling (поле 1.20.5–1.21.1)
	if ss.send(b) != nil {
		return
	}
	if id, _, err := ss.next(); err != nil || id != sbLoginAck {
		return
	}
	ss.fr.MaxLen, ss.fr.MaxData = 4096, 4096

	if !ss.configure(log) {
		return
	}

	// Вход в мир до Finish Configuration: отказ (сервер полон, имя занято)
	// ещё можно сообщить пакетом Disconnect фазы configuration.
	enc := s.enc
	peer := world.NewPeer(world.KindMinecraft, enc, s.cfg.QueueLen)
	eid, err := s.world.Join(ctx, name, uuid, peer)
	if err != nil {
		switch {
		case errors.Is(err, world.ErrFull):
			ss.configDisconnect("Сервер заполнен")
		case errors.Is(err, world.ErrDuplicate):
			ss.configDisconnect("Игрок с таким именем уже на сервере")
		default:
			ss.configDisconnect("Сервер останавливается")
		}
		s.metrics.ConnRejected(protoName, "join")
		return
	}
	if ss.send(ss.pw.Begin(cbConfigFinish)) != nil {
		s.world.Submit(context.WithoutCancel(ctx), world.Command{Kind: world.CmdLeave, Player: eid, Reason: world.ReasonQuit})
		return
	}
	// Ждём подтверждения; всё остальное в configuration игнорируем.
	for {
		id, _, err := ss.next()
		if err != nil {
			s.world.Submit(context.WithoutCancel(ctx), world.Command{Kind: world.CmdLeave, Player: eid, Reason: world.ReasonQuit})
			return
		}
		if id == sbConfigFinish {
			break
		}
	}
	ss.fr.MaxLen, ss.fr.MaxData = s.cfg.MaxPacket, s.cfg.MaxPacket
	if !ss.stopClose() { // сервер уже останавливается — соединение закрыто
		s.world.Submit(context.WithoutCancel(ctx), world.Command{Kind: world.CmdLeave, Player: eid, Reason: world.ReasonShutdown})
		return
	}
	_ = ss.conn.SetDeadline(time.Time{})
	log.Info("minecraft player in game", "id", eid)
	ss.play(ctx, eid, peer, log)
}

// configure — фаза configuration: known packs, реестры, флаги.
func (ss *session) configure(log *slog.Logger) bool {
	b := ss.pw.Begin(cbConfigFeatureFlags)
	b = mcproto.AppendVarInt(b, 1)
	b = mcproto.AppendString(b, "minecraft:vanilla")
	if ss.send(b) != nil {
		return false
	}
	b = ss.pw.Begin(cbConfigKnownPacks)
	b = mcproto.AppendVarInt(b, int32(len(serverPacks)))
	for _, p := range serverPacks {
		b = mcproto.AppendString(b, p.namespace)
		b = mcproto.AppendString(b, p.id)
		b = mcproto.AppendString(b, p.version)
	}
	if ss.send(b) != nil {
		return false
	}
	// Ждём ответ с выбранными наборами. По дороге клиент присылает Client
	// Information и brand — пропускаем.
	for {
		id, payload, err := ss.next()
		if err != nil {
			return false
		}
		if id != sbConfigKnownPacks {
			continue
		}
		d := mcproto.NewDecoder(payload)
		n := d.VarInt()
		hasCore := false
		for i := int32(0); i < n && i < 64 && d.Err() == nil; i++ {
			ns, pid, _ := d.String(64), d.String(64), d.String(64)
			if ns == "minecraft" && pid == "core" {
				hasCore = true
			}
		}
		if d.Err() != nil {
			return false
		}
		if !hasCore {
			// Без ванильных данных клиенту нужно полное содержимое реестров
			// (NBT) — его мы не распространяем.
			log.Info("client has no vanilla data pack", "packs", n)
			ss.configDisconnect("Нужен клиент Minecraft 1.21/1.21.1 с ванильными данными")
			return false
		}
		break
	}
	for _, rp := range ss.s.registryPackets {
		// registryPackets уже содержат id; Frame копирует тело в кадр.
		body := append(ss.pw.Begin(0)[:0], rp...)
		if ss.send(body) != nil {
			return false
		}
	}
	return true
}

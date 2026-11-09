// Пакет gateway — WebSocket-шлюз браузерного клиента. Браузерный игрок —
// полноценный игрок того же мира: его команды идут в тот же входящий
// канал, проходят ту же проверку и те же лимиты, а Minecraft-клиенты видят
// его как игрока с именем.
//
// Протокол (JSON) описан в README, раздел «Протокол WebSocket».
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"gotutor/labs/gameserver/internal/limit"
	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/websocket"
	"gotutor/labs/gameserver/internal/world"
)

// Config — параметры шлюза.
type Config struct {
	Origins           []string      // разрешённые Origin страниц
	MaxConns          int           // одновременных соединений
	MaxMessage        int           // предел входящего сообщения, байт
	MessagesPerSecond float64       // лимит частоты входящих сообщений…
	MessageBurst      float64       // …и запас
	HelloTimeout      time.Duration // на первое сообщение hello
	ReadTimeout       time.Duration // тишина от клиента (ping/pong продлевает)
	WriteTimeout      time.Duration
	PingInterval      time.Duration
	QueueLen          int // очередь исходящих пачек (тиков)
}

// DefaultConfig — настройки по умолчанию.
func DefaultConfig() Config {
	return Config{
		Origins:           []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://localhost:4321", "http://127.0.0.1:4321"},
		MaxConns:          256,
		MaxMessage:        4 << 10,
		MessagesPerSecond: 40,
		MessageBurst:      80,
		HelloTimeout:      5 * time.Second,
		ReadTimeout:       45 * time.Second,
		WriteTimeout:      10 * time.Second,
		PingInterval:      15 * time.Second,
		QueueLen:          64,
	}
}

// Metrics — метрики соединений (та же реализация, что у Minecraft).
type Metrics interface {
	ConnOpened(proto string)
	ConnClosed(proto string)
	ConnRejected(proto, reason string)
	BytesOut(proto string, n int)
	PacketIn(proto string)
}

const protoName = "web"

// Gateway — HTTP-обработчик шлюза.
type Gateway struct {
	cfg     Config
	log     *slog.Logger
	world   *world.World
	enc     *Encoder
	metrics Metrics
	sem     chan struct{}
	wg      sync.WaitGroup
	ctx     context.Context
}

// New создаёт шлюз. ctx — время жизни сервера: при отмене новые входы
// получают отказ.
func New(ctx context.Context, cfg Config, w *world.World, enc *Encoder, m Metrics, log *slog.Logger) *Gateway {
	return &Gateway{cfg: cfg, log: log, world: w, enc: enc, metrics: m, sem: make(chan struct{}, cfg.MaxConns), ctx: ctx}
}

// Handler — маршруты шлюза: /ws — WebSocket, / — подсказка.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", g.serveWS)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("go-tutor game server: WebSocket endpoint is /ws (see labs/gameserver/README.md)\n"))
	})
	return mux
}

// Wait ждёт завершения соединений (не дольше ctx).
func (g *Gateway) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() { g.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (g *Gateway) originAllowed(origin string) bool { return slices.Contains(g.cfg.Origins, origin) }

func (g *Gateway) serveWS(w http.ResponseWriter, r *http.Request) {
	select {
	case g.sem <- struct{}{}:
	default:
		g.metrics.ConnRejected(protoName, "conn_limit")
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	defer func() { <-g.sem }()
	// wg.Add — до Upgrade: http.Server.Shutdown ждёт обработчики, пока
	// соединение не перехвачено, а после перехвата его уже учитывает wg.
	// Иначе Add мог бы гоняться с Wait при остановке.
	g.wg.Add(1)
	defer g.wg.Done()
	conn, err := websocket.Upgrade(w, r, g.originAllowed)
	if err != nil {
		g.metrics.ConnRejected(protoName, "handshake")
		g.log.Info("websocket handshake rejected", "remote", r.RemoteAddr, "origin", r.Header.Get("Origin"), "err", err)
		return
	}
	g.metrics.ConnOpened(protoName)
	defer g.metrics.ConnClosed(protoName)
	defer conn.Close()
	conn.MaxMessage = g.cfg.MaxMessage
	conn.WriteTimeout = g.cfg.WriteTimeout
	// Лимит на каждый кадр, включая ping и пустые фрагменты: лимит
	// сообщений (readLoop) их не видит. Вдвое мягче лимита сообщений.
	frames := limit.New(2*g.cfg.MessagesPerSecond, 2*g.cfg.MessageBurst, time.Now())
	conn.AllowFrame = func() bool { return frames.Allow(time.Now()) }
	g.session(conn, g.log.With("remote", r.RemoteAddr))
}

// clientMsg — сообщение клиента. Поля-указатели — необязательные.
type clientMsg struct {
	T     string   `json:"t"`
	Name  string   `json:"name"`
	X     *float64 `json:"x"`
	Y     *float64 `json:"y"`
	Z     *float64 `json:"z"`
	Yaw   *float32 `json:"yaw"`
	Pitch *float32 `json:"pitch"`
	Text  string   `json:"text"`
	TP    int32    `json:"tp"`
	Block string   `json:"block"`
}

// kick отправляет одно сообщение [{"t":"kick"}] (без tick: вне тика) и
// close. Вызывается из горутин соединения, поэтому не трогает общий
// Encoder, которым владеет тик, — только чистую функцию kickMessage.
func (g *Gateway) kick(conn *websocket.Conn, reason string, code int) {
	_ = conn.WriteMessage(websocket.OpText, kickMessage(reason))
	_ = conn.WriteClose(code, "")
}

func (g *Gateway) session(conn *websocket.Conn, log *slog.Logger) {
	// 1. hello с именем — за HelloTimeout. Дедлайн абсолютный: ping и
	// фрагменты не продлевают его (ReadTimeout продлевался бы на каждый кадр).
	// Остановка сервера до hello закрывает соединение сразу.
	conn.ReadTimeout = 0
	_ = conn.SetReadDeadline(time.Now().Add(g.cfg.HelloTimeout))
	stopOnShutdown := context.AfterFunc(g.ctx, func() { conn.Close() })
	_, data, err := conn.ReadMessage()
	if !stopOnShutdown() || err != nil {
		return
	}
	g.metrics.PacketIn(protoName)
	var hello clientMsg
	if json.Unmarshal(data, &hello) != nil || hello.T != "hello" {
		g.kick(conn, "first message must be {\"t\":\"hello\",\"name\":...}", websocket.ClosePolicy)
		return
	}
	if !world.ValidName(hello.Name) {
		g.metrics.ConnRejected(protoName, "bad_name")
		g.kick(conn, "invalid name: 3-16 characters A-Z, a-z, 0-9, _", websocket.ClosePolicy)
		return
	}
	peer := world.NewPeer(world.KindWeb, g.enc, g.cfg.QueueLen)
	eid, err := g.world.Join(g.ctx, hello.Name, mcproto.OfflineUUID(hello.Name), peer)
	if err != nil {
		g.metrics.ConnRejected(protoName, "join")
		g.kick(conn, err.Error(), websocket.CloseGoingAway)
		return
	}
	log = log.With("name", hello.Name, "id", eid)
	log.Info("web player in game")

	// 2. Игра: запись — в своей горутине, чтение — здесь.
	done := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		g.writeLoop(conn, peer, done)
	}()
	conn.ReadTimeout = g.cfg.ReadTimeout
	reason := g.readLoop(conn, eid, peer)
	g.world.Submit(context.WithoutCancel(g.ctx), world.Command{Kind: world.CmdLeave, Player: eid, Reason: reason})
	close(done)
	<-writerDone
	if r := peer.Reason(); r != "" {
		reason = r
	}
	log.Info("web connection closed", "reason", reason)
}

func (g *Gateway) readLoop(conn *websocket.Conn, eid world.EntityID, peer *world.Peer) string {
	rate := limit.New(g.cfg.MessagesPerSecond, g.cfg.MessageBurst, time.Now())
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			g.log.Debug("websocket read", "id", eid, "err", err)
			return readErrReason(err)
		}
		g.metrics.PacketIn(protoName)
		if !rate.Allow(time.Now()) {
			g.kick(conn, "too many messages", websocket.ClosePolicy)
			return world.ReasonRateLimit
		}
		var m clientMsg
		if err := json.Unmarshal(data, &m); err != nil {
			g.kick(conn, "malformed message", websocket.CloseInvalidPayload)
			return world.ReasonBadPacket
		}
		cmd := world.Command{Player: eid}
		switch m.T {
		case "move":
			if m.X == nil || m.Z == nil {
				g.kick(conn, "move needs x and z", websocket.CloseInvalidPayload)
				return world.ReasonBadPacket
			}
			cmd.Kind = world.CmdMove
			cmd.HasPos = true
			cmd.Pos = world.Vec3{X: *m.X, Y: world.SpawnY, Z: *m.Z}
			if m.Y != nil {
				cmd.Pos.Y = *m.Y
			}
			if m.Yaw != nil || m.Pitch != nil {
				cmd.HasRot = true
				if m.Yaw != nil {
					cmd.Yaw = *m.Yaw
				}
				if m.Pitch != nil {
					cmd.Pitch = *m.Pitch
				}
			}
		case "tpack":
			cmd.Kind, cmd.TeleportID = world.CmdTeleportAck, m.TP
		case "chat":
			cmd.Kind, cmd.Text = world.CmdChat, m.Text
		case "dig", "place":
			if m.X == nil || m.Y == nil || m.Z == nil {
				g.kick(conn, m.T+" needs x, y and z", websocket.CloseInvalidPayload)
				return world.ReasonBadPacket
			}
			cmd.Block = world.BlockPos{X: blockCoord(*m.X), Y: blockCoord(*m.Y), Z: blockCoord(*m.Z)}
			cmd.Kind = world.CmdDig
			if m.T == "place" {
				cmd.Kind = world.CmdPlace
				cmd.State, _ = world.DefaultState(m.Block) // неизвестное имя → воздух → отказ мира
			}
		default:
			continue // неизвестные типы пропускаем: клиент может быть новее сервера
		}
		if !g.world.Submit(g.ctx, cmd) {
			return world.ReasonShutdown
		}
		select {
		case <-peer.Closed():
			return peer.Reason()
		default:
		}
	}
}

// blockCoord — координата блока из числа JSON: вниз до целого, с
// ограничением (int32(1e30) в Go — неопределённое значение, а не ошибка).
func blockCoord(v float64) int32 {
	return int32(math.Max(-1e7, math.Min(1e7, math.Floor(v))))
}

func readErrReason(err error) string {
	var ce *websocket.CloseError
	var ne net.Error
	switch {
	case errors.As(err, &ce):
		return world.ReasonQuit
	case errors.Is(err, websocket.ErrRateLimited):
		return world.ReasonRateLimit
	case errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return world.ReasonTimeout
	case errors.Is(err, websocket.ErrProtocol), errors.Is(err, websocket.ErrTooBig), errors.Is(err, websocket.ErrInvalidUTF8):
		return world.ReasonBadPacket
	}
	return world.ReasonQuit
}

func (g *Gateway) writeLoop(conn *websocket.Conn, peer *world.Peer, done <-chan struct{}) {
	ticker := time.NewTicker(g.cfg.PingInterval)
	defer ticker.Stop()
	write := func(op byte, b []byte) bool {
		if err := conn.WriteMessage(op, b); err != nil {
			return false
		}
		g.metrics.BytesOut(protoName, len(b))
		return true
	}
	for {
		select {
		case b := <-peer.Out():
			ok := write(websocket.OpText, b.B)
			world.PutBuffer(b)
			if !ok {
				conn.Close()
				return
			}
		case <-ticker.C:
			if !write(websocket.OpPing, nil) {
				conn.Close()
				return
			}
		case <-peer.Closed():
			// Дописываем очередь (там kick с причиной) и закрываем.
			for {
				select {
				case b := <-peer.Out():
					ok := write(websocket.OpText, b.B)
					world.PutBuffer(b)
					if !ok {
						conn.Close()
						return
					}
				default:
					code := websocket.ClosePolicy
					if peer.Reason() == world.ReasonShutdown {
						code = websocket.CloseGoingAway
					}
					_ = conn.WriteClose(code, peer.Reason())
					conn.Close()
					return
				}
			}
		case <-done:
			return
		}
	}
}

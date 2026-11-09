package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"gotutor/labs/gameserver/internal/websocket"
	"gotutor/labs/gameserver/internal/world"
)

func TestEncoderProducesValidJSON(t *testing.T) {
	e := NewEncoder()
	p := &world.PlayerInfo{ID: 3, Name: "alice", Kind: world.KindWeb, Pos: world.Vec3{X: 1.23456, Y: -60, Z: -7}}
	wl := &world.Welcome{ViewDistance: 6, Placeable: []string{"minecraft:stone"}, TickRate: 20}
	b := e.Begin(nil)
	for _, ev := range []world.Event{
		{Kind: world.EvWelcome, Player: p, Welcome: wl, TeleportID: 1},
		{Kind: world.EvRosterAdd, Player: p},
		{Kind: world.EvSpawn, Player: p},
		{Kind: world.EvMove, Player: p, To: p.Pos, Yaw: 90},
		{Kind: world.EvChat, Player: p, Text: "кавычка \" слеш \\ <tag> \u2028 табуляция\t"},
		{Kind: world.EvChunk, Chunk: world.ChunkPos{X: -1, Z: 2}},
		{Kind: world.EvChunkBatchBegin}, // браузеру не нужно — ничего не пишет
		{Kind: world.EvBlock, Block: world.BlockPos{X: 1, Y: 2, Z: 3}, State: world.Stone, TopY: 2, TopState: world.Stone},
		{Kind: world.EvTeleport, Player: p, To: p.Pos, TeleportID: 2, Reason: "too_fast"},
		{Kind: world.EvDespawn, Player: p},
	} {
		b = e.Append(b, &ev)
	}
	b = e.End(b, &world.BatchStats{Tick: 10, MSPT: 0.5, TPS: 20, Events: 9})
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, b)
	}
	if len(out) != 10 || out[9]["t"] != "tick" || out[9]["msgs"] != 9.0 {
		t.Fatalf("events: %v", out)
	}
	if out[4]["text"] != "кавычка \" слеш \\ <tag> \u2028 табуляция\t" {
		t.Fatalf("chat roundtrip: %q", out[4]["text"])
	}
	if out[3]["x"] != 1.235 {
		t.Fatalf("coordinates are rounded to 1e-3: %v", out[3]["x"])
	}
	chunk := out[5]
	if len(chunk["top"].([]any)) != 256 || len(chunk["height"].([]any)) != 256 {
		t.Fatal("chunk must have 256 columns")
	}
	if pal := chunk["palette"].([]any); len(pal) != 2 { // трава и камень на границах
		t.Fatalf("palette %v", pal)
	}
}

func FuzzAppendString(f *testing.F) {
	f.Add("hello")
	f.Add("\x00\x1f\"\\<>&\u2028\u2029")
	f.Add("\xff\xfe")
	f.Fuzz(func(t *testing.T, s string) {
		b := appendString(nil, s)
		var got string
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("invalid JSON for %q: %s", s, b)
		}
		if utf8.ValidString(s) && got != s {
			t.Fatalf("roundtrip %q → %q", s, got)
		}
	})
}

// --------------------------------------------- интеграция на loopback

func startGateway(t *testing.T, mod func(*Config)) string {
	url, _ := startGatewayG(t, mod)
	return url
}

func startGatewayG(t *testing.T, mod func(*Config)) (string, *Gateway) {
	t.Helper()
	cfg := DefaultConfig()
	if mod != nil {
		mod(&cfg)
	}
	wcfg := world.DefaultConfig()
	wcfg.ChunksPerTick = 64
	w := world.New(wcfg, world.Options{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	worldDone := make(chan struct{})
	go func() { w.Run(ctx, "bye"); close(worldDone) }()
	g := New(ctx, cfg, w, NewEncoder(), nopMetrics{}, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(g.Handler())
	t.Cleanup(func() {
		cancel()
		<-worldDone
		srv.Close()
		wctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		g.Wait(wctx)
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", g
}

type nopMetrics struct{}

func (nopMetrics) ConnOpened(string)           {}
func (nopMetrics) ConnClosed(string)           {}
func (nopMetrics) ConnRejected(string, string) {}
func (nopMetrics) BytesOut(string, int)        {}
func (nopMetrics) PacketIn(string)             {}

type client struct {
	t      *testing.T
	conn   *websocket.Conn
	mu     sync.Mutex
	events []map[string]any
	closed chan error
}

func connect(t *testing.T, url, origin, hello string) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := websocket.Dial(ctx, url, origin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &client{t: t, conn: conn, closed: make(chan error, 1)}
	if hello != "" {
		c.send(hello)
	}
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				c.closed <- err
				return
			}
			var batch []map[string]any
			if err := json.Unmarshal(data, &batch); err != nil {
				c.closed <- err
				return
			}
			c.mu.Lock()
			c.events = append(c.events, batch...)
			c.mu.Unlock()
		}
	}()
	return c
}

func (c *client) send(s string) {
	if err := c.conn.WriteMessage(websocket.OpText, []byte(s)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) wait(what string, match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, ev := range c.events {
			if match(ev) {
				c.mu.Unlock()
				return ev
			}
		}
		c.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	c.t.Fatalf("timeout waiting for %s", what)
	return nil
}

func isT(t string) func(map[string]any) bool {
	return func(ev map[string]any) bool { return ev["t"] == t }
}

func TestWebPlayerFlow(t *testing.T) {
	url := startGateway(t, nil)
	c := connect(t, url, "http://localhost:8080", `{"t":"hello","name":"webby"}`)
	wl := c.wait("welcome", isT("welcome"))
	c.wait("chunk", isT("chunk"))
	c.wait("tick stats", isT("tick"))
	c.send(fmt.Sprintf(`{"t":"tpack","tp":%v}`, wl["tp"]))

	// Слишком быстро — поправка с новым tp, как у Minecraft-клиента.
	x, z := wl["x"].(float64), wl["z"].(float64)
	c.send(fmt.Sprintf(`{"t":"move","x":%f,"z":%f}`, x+200, z))
	tp := c.wait("teleport", isT("teleport"))
	if tp["reason"] != "too_fast" {
		t.Fatalf("teleport %v", tp)
	}
	c.send(fmt.Sprintf(`{"t":"tpack","tp":%v}`, tp["tp"]))

	// Свой чат приходит обратно.
	c.send(`{"t":"chat","text":"hello world"}`)
	c.wait("own chat", func(ev map[string]any) bool { return ev["t"] == "chat" && ev["text"] == "hello world" })

	// Поставить блок рядом и сломать его.
	bx, bz := int(x)+1, int(z)
	c.send(fmt.Sprintf(`{"t":"place","x":%d,"y":-60,"z":%d,"block":"minecraft:gold_block"}`, bx, bz))
	c.wait("block placed", func(ev map[string]any) bool { return ev["t"] == "block" && ev["block"] == "minecraft:gold_block" })
	// Блок не из палитры браузера — отказ (приходит настоящее состояние — воздух).
	c.send(fmt.Sprintf(`{"t":"place","x":%d,"y":-60,"z":%d,"block":"minecraft:tnt"}`, bx+1, bz))
	c.wait("tnt rejected", func(ev map[string]any) bool {
		return ev["t"] == "block" && ev["block"] == "minecraft:air" && ev["x"] == float64(bx+1)
	})
}

func TestForeignOriginRejected(t *testing.T) {
	url := startGateway(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := websocket.Dial(ctx, url, "https://evil.example"); err == nil {
		t.Fatal("foreign origin must be rejected")
	}
}

func TestBadHelloAndFlood(t *testing.T) {
	url := startGateway(t, func(c *Config) { c.MessagesPerSecond = 5; c.MessageBurst = 5 })
	c := connect(t, url, "", `{"t":"hello","name":"x"}`) // имя короче 3 символов
	c.wait("kick", isT("kick"))
	<-c.closed

	f := connect(t, url, "", `{"t":"hello","name":"flooder"}`)
	f.wait("welcome", isT("welcome"))
	for i := 0; i < 50; i++ {
		if f.conn.WriteMessage(websocket.OpText, []byte(`{"t":"chat","text":"spam"}`)) != nil {
			break
		}
	}
	f.wait("kick for flood", func(ev map[string]any) bool { return ev["t"] == "kick" && ev["reason"] == "too many messages" })
	<-f.closed
}

// rawWS — рукопожатие и сырой сокет: клиент, который ведёт себя плохо.
func rawWS(t *testing.T, url string) net.Conn {
	t.Helper()
	host := strings.TrimSuffix(strings.TrimPrefix(url, "ws://"), "/ws")
	c, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	req := "GET /ws HTTP/1.1\r\nHost: " + host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake: %v %v", resp, err)
	}
	return c
}

// maskedPing — кадр ping клиента (маска нулевая — допустимо).
func maskedPing() []byte { return []byte{0x80 | websocket.OpPing, 0x80, 0, 0, 0, 0} }

func waitSlotsFree(t *testing.T, g *Gateway, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for len(g.sem) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("connection slot not released within %s", within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestPingsDoNotExtendHelloDeadline — ping раз в 50 мс без hello: дедлайн
// hello абсолютный, соединение закрывается, слот освобождается.
func TestPingsDoNotExtendHelloDeadline(t *testing.T) {
	url, g := startGatewayG(t, func(c *Config) { c.HelloTimeout = 300 * time.Millisecond })
	c := rawWS(t, url)
	go io.Copy(io.Discard, c) // ответы читаем: проверяем именно дедлайн
	stop := time.After(2 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for loop := true; loop; {
		select {
		case <-tick.C:
			if _, err := c.Write(maskedPing()); err != nil {
				loop = false
			}
		case <-stop:
			loop = false
		}
	}
	waitSlotsFree(t, g, time.Second)
}

// TestPingFloodWithoutReading — поток ping без чтения pong: лимит кадров и
// дедлайн записи закрывают соединение, слот не утекает.
func TestPingFloodWithoutReading(t *testing.T) {
	url, g := startGatewayG(t, func(c *Config) { c.WriteTimeout = 300 * time.Millisecond })
	c := rawWS(t, url)
	var batch []byte
	for i := 0; i < 2000; i++ {
		batch = append(batch, maskedPing()...)
	}
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = c.Write(batch) // ответы не читаем
	waitSlotsFree(t, g, 3*time.Second)
}

// TestTickBytes — tick.bytes: события вместе с запятыми, без '[' и tick
// (пример из README: один move — 67 байт).
func TestTickBytes(t *testing.T) {
	e := NewEncoder()
	p := &world.PlayerInfo{ID: 5}
	b := e.Begin(nil)
	b = e.Append(b, &world.Event{Kind: world.EvMove, Player: p, To: world.Vec3{X: 12.345, Y: -60, Z: -3.2}, Yaw: 90})
	b = e.End(b, &world.BatchStats{Events: 1})
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out[1]["bytes"] != 67.0 {
		t.Fatalf("bytes = %v in %s", out[1]["bytes"], b)
	}
	b = e.End(e.Begin(nil), &world.BatchStats{})
	if !strings.Contains(string(b), `"msgs":0,"bytes":0`) {
		t.Fatalf("empty batch: %s", b)
	}
}

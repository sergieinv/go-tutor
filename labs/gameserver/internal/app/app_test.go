package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotutor/labs/gameserver/internal/websocket"
)

// start поднимает сервер с сохранением в path.
func start(t *testing.T, path string) (Addrs, func()) {
	t.Helper()
	cfg, err := LoadConfig(func(k string) string {
		switch k {
		case "GAME_SAVE_PATH":
			return path
		case "GAME_SAVE_INTERVAL":
			return "1h" // только финальное сохранение
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.MCAddr, cfg.WSAddr, cfg.OpsAddr = "127.0.0.1:0", "127.0.0.1:0", "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan Addrs, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, slog.New(slog.DiscardHandler), func(a Addrs) { ready <- a }) }()
	select {
	case a := <-ready:
		return a, func() {
			cancel()
			if err := <-done; err != nil {
				t.Errorf("run: %v", err)
			}
		}
	case err := <-done:
		t.Fatal(err)
	}
	return Addrs{}, nil
}

// webSession — браузерный игрок: подключиться, дождаться welcome.
type webSession struct {
	conn *websocket.Conn
	evs  chan map[string]any
}

func web(t *testing.T, addr, name string) *webSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := websocket.Dial(ctx, "ws://"+addr+"/ws", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &webSession{conn: conn, evs: make(chan map[string]any, 4096)}
	conn.WriteMessage(websocket.OpText, []byte(`{"t":"hello","name":"`+name+`"}`))
	go func() {
		defer close(s.evs)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var batch []map[string]any
			json.Unmarshal(data, &batch)
			for _, ev := range batch {
				select {
				case s.evs <- ev:
				default:
				}
			}
		}
	}()
	return s
}

func (s *webSession) wait(t *testing.T, typ string) map[string]any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-s.evs:
			if !ok {
				t.Fatalf("connection closed waiting for %s", typ)
			}
			if ev["t"] == typ {
				return ev
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %s", typ)
		}
	}
}

// TestGracefulShutdownSaves — остановка (как SIGTERM от docker stop):
// игрок получает kick, мир сохраняется, после перезапуска блок на месте,
// а игрок появляется там, где вышел.
func TestGracefulShutdownSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.json")
	a, stop := start(t, path)

	// Служебный порт отвечает.
	for _, p := range []string{"/healthz", "/readyz", "/metrics"} {
		resp, err := http.Get("http://" + a.Ops + p)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", p, resp, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if p == "/metrics" && !strings.Contains(string(body), "gameserver_tick_duration_seconds") {
			t.Fatal("metrics missing")
		}
	}

	s := web(t, a.WS, "saver")
	wl := s.wait(t, "welcome")
	s.conn.WriteMessage(websocket.OpText, []byte(fmt.Sprintf(`{"t":"tpack","tp":%v}`, wl["tp"])))
	x, z := wl["x"].(float64), wl["z"].(float64)
	time.Sleep(100 * time.Millisecond)
	s.conn.WriteMessage(websocket.OpText, []byte(fmt.Sprintf(`{"t":"move","x":%f,"z":%f}`, x+2, z)))
	bx, bz := int(x)-1, int(z)
	s.conn.WriteMessage(websocket.OpText, []byte(fmt.Sprintf(`{"t":"place","x":%d,"y":-60,"z":%d,"block":"minecraft:diamond_block"}`, bx, bz)))
	s.wait(t, "block")

	stop()
	if ev := s.wait(t, "kick"); !strings.Contains(ev["reason"].(string), "shutting down") {
		t.Fatalf("kick %v", ev)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"name":"saver"`) || !strings.Contains(string(data), `"blocks":[{`) {
		t.Fatalf("snapshot: %s", data)
	}

	a, stop = start(t, path)
	defer stop()
	s = web(t, a.WS, "saver")
	wl = s.wait(t, "welcome")
	if wl["x"].(float64) != x+2 {
		t.Fatalf("rejoined at x=%v, want %v", wl["x"], x+2)
	}
	for {
		ch := s.wait(t, "chunk")
		if ch["x"].(float64) != float64(bx>>4) || ch["z"].(float64) != float64(bz>>4) {
			continue
		}
		pal := ch["palette"].([]any)
		top := ch["top"].([]any)[(bz&15)*16+(bx&15)].(float64)
		if pal[int(top)] != "minecraft:diamond_block" {
			t.Fatalf("restored block = %v", pal[int(top)])
		}
		return
	}
}

// TestShutdownWithStuckHandshakes — соединения, застрявшие до входа
// (Minecraft после handshake, WebSocket без hello), не задерживают
// остановку: их закрывает отмена контекста, а не таймауты входа.
func TestShutdownWithStuckHandshakes(t *testing.T) {
	a, stop := start(t, filepath.Join(t.TempDir(), "w.json"))
	mc, err := net.Dial("tcp", a.MC)
	if err != nil {
		t.Fatal(err)
	}
	defer mc.Close()
	mc.Write([]byte{0x02, 0x00, 0x00}) // начало handshake, дальше тишина
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, err := websocket.Dial(ctx, "ws://"+a.WS+"/ws", "")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	stop()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("shutdown took %s with stuck handshakes", d)
	}
}

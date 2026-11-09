// Пакет e2e — сквозная проверка совместимости: настоящий клиент протокола
// 767 из go-mc (bot) входит на сервер, получает мир и чанки, видит другого
// бота и браузерного игрока, пишет в чат и ходит.
//
// По умолчанию сервер поднимается в процессе на 127.0.0.1 со случайными
// портами. Против контейнера:
//
//	GAME_E2E_MC=127.0.0.1:25565 GAME_E2E_WS=127.0.0.1:25580 go test ./internal/e2e/ -run . -v
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tnze/go-mc/bot"
	"github.com/Tnze/go-mc/bot/basic"
	"github.com/Tnze/go-mc/bot/msg"
	"github.com/Tnze/go-mc/bot/playerlist"
	"github.com/Tnze/go-mc/chat"
	"github.com/Tnze/go-mc/data/packetid"
	"github.com/Tnze/go-mc/level"
	"github.com/Tnze/go-mc/level/block"
	pk "github.com/Tnze/go-mc/net/packet"
	"github.com/google/uuid"

	"gotutor/labs/gameserver/internal/app"
	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/websocket"
	"gotutor/labs/gameserver/internal/world"
)

// server — адреса сервера под тестом.
type server struct{ mc, ws string }

func startServer(t *testing.T) server {
	t.Helper()
	if mc := os.Getenv("GAME_E2E_MC"); mc != "" {
		return server{mc: mc, ws: os.Getenv("GAME_E2E_WS")}
	}
	cfg, err := app.LoadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	cfg.MCAddr, cfg.WSAddr, cfg.OpsAddr = "127.0.0.1:0", "127.0.0.1:0", "127.0.0.1:0"
	cfg.World.ChunksPerTick = 64
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan app.Addrs, 1)
	done := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	go func() { done <- app.Run(ctx, cfg, log, func(a app.Addrs) { ready <- a }) }()
	var a app.Addrs
	select {
	case a = <-ready:
	case err := <-done:
		t.Fatalf("server failed: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("server stop: %v", err)
		}
	})
	return server{mc: a.MC, ws: a.WS}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}

// ---------------------------------------------------------- бот go-mc

// knownPacks — бот «знает» ванильный набор данных (как настоящий клиент),
// иначе сервер откажет: содержимое реестров он не присылает.
type knownPacks struct{ *bot.DefaultConfigHandler }

func (knownPacks) SelectDataPacks(packs []bot.DataPack) []bot.DataPack {
	var out []bot.DataPack
	for _, p := range packs {
		if p.Namespace == "minecraft" && p.ID == "core" {
			out = append(out, p)
		}
	}
	return out
}

type mcBot struct {
	name string
	c    *bot.Client
	p    *basic.Player
	pl   *playerlist.PlayerList
	chat *msg.Manager

	mu        sync.Mutex
	chunks    int
	chunkErr  error
	grassOK   bool
	entities  map[int32]uuid.UUID
	removed   map[int32]bool
	moves     map[int32]int
	messages  []string
	names     map[uuid.UUID]string // копия списка игроков (Tab), под mu
	pos       [3]float64
	teleports int
	done      chan error
}

func newBot(t *testing.T, addr, name string) *mcBot {
	t.Helper()
	b := &mcBot{name: name, entities: map[int32]uuid.UUID{}, removed: map[int32]bool{}, moves: map[int32]int{}, names: map[uuid.UUID]string{}, done: make(chan error, 1)}
	c := bot.NewClient()
	c.Auth.Name = name
	c.ConfigHandler = knownPacks{bot.NewDefaultConfigHandler()}
	b.c = c
	b.p = basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{
		Teleported: func(x, y, z float64, yaw, pitch float32, flag byte, id int32) error {
			b.mu.Lock()
			b.pos = [3]float64{x, y, z}
			b.teleports++
			b.mu.Unlock()
			return b.p.AcceptTeleportation(pk.VarInt(id))
		},
	})
	b.pl = playerlist.New(c)
	b.chat = msg.New(c, b.p, b.pl, msg.EventsHandler{
		SystemChat: func(m chat.Message, overlay bool) error {
			b.mu.Lock()
			b.messages = append(b.messages, m.ClearString())
			b.mu.Unlock()
			return nil
		},
	})
	c.Events.AddListener(
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundLevelChunkWithLight, F: b.onChunk},
		// После playerlist (приоритет 64) копируем имена: читать PlayerInfos
		// из горутины теста было бы гонкой.
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayerInfoUpdate, F: func(pk.Packet) error {
			b.mu.Lock()
			for u, info := range b.pl.PlayerInfos {
				b.names[u] = info.Name
			}
			b.mu.Unlock()
			return nil
		}},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundAddEntity, F: b.onAddEntity},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundRemoveEntities, F: b.onRemove},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundMoveEntityPos, F: b.onMove},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundMoveEntityPosRot, F: b.onMove},
		bot.PacketHandler{Priority: 0, ID: packetid.ClientboundTeleportEntity, F: b.onMove},
	)
	if err := c.JoinServer(addr); err != nil {
		t.Fatalf("%s join: %v", name, err)
	}
	go func() { b.done <- c.HandleGame() }()
	t.Cleanup(func() { c.Close() })
	return b
}

// onChunk разбирает чанк парсером go-mc: так проверяется, что формат
// секций, палитр, карты высот и света совпадает с протоколом 767.
func (b *mcBot) onChunk(p pk.Packet) error {
	r := bytes.NewReader(p.Data)
	var pos level.ChunkPos
	ch := level.EmptyChunk(world.SectionCount)
	_, err := pos.ReadFrom(r)
	if err == nil {
		_, err = ch.ReadFrom(r)
	}
	if err == nil && r.Len() != 0 {
		err = fmt.Errorf("chunk %v: %d trailing bytes", pos, r.Len())
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.chunkErr = err
		return nil
	}
	b.chunks++
	// Верхний слой: (1, GroundY, 1) — трава, (0, GroundY, 0) — камень границы чанка.
	sec := &ch.Sections[(world.GroundY-world.MinY)/16]
	y := (world.GroundY - world.MinY) % 16
	grass := sec.GetBlock(y<<8 | 1<<4 | 1)
	stone := sec.GetBlock(y << 8)
	air := ch.Sections[5].GetBlock(0)
	if grass == block.StateID(world.GrassBlock) && stone == block.StateID(world.Stone) && air == 0 {
		b.grassOK = true
	} else {
		b.chunkErr = fmt.Errorf("unexpected blocks: grass=%d stone=%d air=%d", grass, stone, air)
	}
	return nil
}

func (b *mcBot) onAddEntity(p pk.Packet) error {
	var id, typ pk.VarInt
	var u pk.UUID
	if err := p.Scan(&id, &u, &typ); err != nil {
		return err
	}
	if typ == 128 { // minecraft:player
		b.mu.Lock()
		b.entities[int32(id)] = uuid.UUID(u)
		delete(b.removed, int32(id))
		b.mu.Unlock()
	}
	return nil
}

func (b *mcBot) onRemove(p pk.Packet) error {
	var ids []pk.VarInt
	if err := p.Scan(pk.Array(&ids)); err != nil {
		return err
	}
	b.mu.Lock()
	for _, id := range ids {
		b.removed[int32(id)] = true
	}
	b.mu.Unlock()
	return nil
}

func (b *mcBot) onMove(p pk.Packet) error {
	var id pk.VarInt
	if err := p.Scan(&id); err != nil {
		return err
	}
	b.mu.Lock()
	b.moves[int32(id)]++
	b.mu.Unlock()
	return nil
}

func (b *mcBot) seesUUID(u [16]byte) (int32, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, eu := range b.entities {
		if eu == uuid.UUID(u) && !b.removed[id] {
			return id, true
		}
	}
	return 0, false
}

func (b *mcBot) hasMessage(sub string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.messages {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func (b *mcBot) move(dx, dz float64) error {
	b.mu.Lock()
	b.pos[0] += dx
	b.pos[2] += dz
	x, y, z := b.pos[0], b.pos[1], b.pos[2]
	b.mu.Unlock()
	return b.c.Conn.WritePacket(pk.Marshal(packetid.ServerboundMovePlayerPos,
		pk.Double(x), pk.Double(y), pk.Double(z), pk.Boolean(true)))
}

// ------------------------------------------------------- клиент WebSocket

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
	evs  []map[string]any
}

func newWS(t *testing.T, addr, name string) *wsClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := websocket.Dial(ctx, "ws://"+addr+"/ws", "http://localhost:8080")
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	c := &wsClient{conn: conn}
	if err := conn.WriteMessage(websocket.OpText, []byte(`{"t":"hello","name":"`+name+`"}`)); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var batch []map[string]any
			if err := json.Unmarshal(data, &batch); err != nil {
				panic(fmt.Sprintf("bad server json %q: %v", data, err))
			}
			c.mu.Lock()
			for _, ev := range batch {
				if ev["t"] != "tick" {
					c.evs = append(c.evs, ev)
				}
				if ev["t"] == "welcome" || ev["t"] == "teleport" {
					_ = conn.WriteMessage(websocket.OpText, []byte(fmt.Sprintf(`{"t":"tpack","tp":%v}`, ev["tp"])))
				}
			}
			c.mu.Unlock()
		}
	}()
	t.Cleanup(func() { conn.Close() })
	return c
}

func (c *wsClient) find(match func(map[string]any) bool) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ev := range c.evs {
		if match(ev) {
			return ev
		}
	}
	return nil
}

func (c *wsClient) has(t, key string, val any) bool {
	return c.find(func(ev map[string]any) bool { return ev["t"] == t && (key == "" || ev[key] == val) }) != nil
}

// ------------------------------------------------------------------ тесты

func TestServerListPing(t *testing.T) {
	s := startServer(t)
	data, _, err := bot.PingAndListTimeout(s.mc, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players     struct{ Max, Online int } `json:"players"`
		Description struct{ Text string }     `json:"description"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st.Version.Protocol != 767 || st.Version.Name != "1.21.1" {
		t.Fatalf("version = %+v", st.Version)
	}
	if !strings.Contains(st.Description.Text, "учебный") || st.Players.Max == 0 {
		t.Fatalf("status = %+v", st)
	}
}

func TestBotsAndBrowserShareWorld(t *testing.T) {
	s := startServer(t)
	a := newBot(t, s.mc, "e2eAlice")
	eventually(t, "alice gets chunks and teleport", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.chunks >= 9 && a.teleports > 0
	})
	a.mu.Lock()
	if a.chunkErr != nil || !a.grassOK {
		t.Fatalf("chunk check: %v", a.chunkErr)
	}
	a.mu.Unlock()
	if a.p.EID == 0 || a.p.Gamemode != 1 || !a.p.IsFlat {
		t.Fatalf("join game: eid=%d mode=%d flat=%v", a.p.EID, a.p.Gamemode, a.p.IsFlat)
	}

	b := newBot(t, s.mc, "e2eBob")
	eventually(t, "bots see each other", func() bool {
		_, ab := a.seesUUID(mcproto.OfflineUUID("e2eBob"))
		_, ba := b.seesUUID(mcproto.OfflineUUID("e2eAlice"))
		return ab && ba
	})

	var web *wsClient
	if s.ws != "" {
		web = newWS(t, s.ws, "e2eWeb")
		eventually(t, "web player sees both bots and gets chunks", func() bool {
			return web.has("welcome", "name", "e2eWeb") && web.has("chunk", "", nil) &&
				web.has("spawn", "name", "e2eAlice") && web.has("spawn", "name", "e2eBob")
		})
		eventually(t, "bots see web player as an entity and in the tab list", func() bool {
			_, ok := a.seesUUID(mcproto.OfflineUUID("e2eWeb"))
			a.mu.Lock()
			name := a.names[uuid.UUID(mcproto.OfflineUUID("e2eWeb"))]
			a.mu.Unlock()
			return ok && name == "e2eWeb"
		})
	}

	// Чат: бот → все (видящие), браузер → боты.
	if err := a.chat.SendMessage("привет из бота"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "bob gets alice's chat", func() bool { return b.hasMessage("<e2eAlice> привет из бота") })
	if web != nil {
		eventually(t, "web gets alice's chat", func() bool { return web.has("chat", "text", "привет из бота") })
		if err := web.conn.WriteMessage(websocket.OpText, []byte(`{"t":"chat","text":"hi from browser"}`)); err != nil {
			t.Fatal(err)
		}
		eventually(t, "bots get browser chat", func() bool { return a.hasMessage("<e2eWeb> hi from browser") })
	}

	// Перемещение: Боб видит пакеты движения Алисы, браузер — move.
	aliceID, _ := b.seesUUID(mcproto.OfflineUUID("e2eAlice"))
	for i := 0; i < 5; i++ {
		if err := a.move(0.5, 0); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Millisecond)
	}
	eventually(t, "bob sees alice move", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.moves[aliceID] > 0
	})
	if web != nil {
		eventually(t, "web sees alice move", func() bool { return web.has("move", "id", float64(aliceID)) })
		// Браузерный игрок ходит — боты видят.
		welcome := web.find(func(ev map[string]any) bool { return ev["t"] == "welcome" })
		x, z := welcome["x"].(float64), welcome["z"].(float64)
		webID, _ := a.seesUUID(mcproto.OfflineUUID("e2eWeb"))
		for i := 1; i <= 5; i++ {
			_ = web.conn.WriteMessage(websocket.OpText, []byte(fmt.Sprintf(`{"t":"move","x":%f,"z":%f,"yaw":90}`, x+float64(i)*0.3, z)))
			time.Sleep(60 * time.Millisecond)
		}
		eventually(t, "alice sees web move", func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			return a.moves[webID] > 0
		})
	}

	// Выход: Боб пропадает у Алисы.
	bobID, _ := a.seesUUID(mcproto.OfflineUUID("e2eBob"))
	b.c.Close()
	eventually(t, "alice sees bob removed", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.removed[bobID]
	})
}

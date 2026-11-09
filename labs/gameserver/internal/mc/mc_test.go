package mc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tnze/go-mc/level"
	"github.com/Tnze/go-mc/level/block"

	"gotutor/labs/gameserver/internal/mcproto"
	"gotutor/labs/gameserver/internal/world"
)

// TestPacketIDs сверяет номера пакетов из go-mc с таблицей протокола 767
// (minecraft.wiki, Java Edition protocol, 1.21.1). Если go-mc обновят на
// другую версию игры, упадёт этот тест, а не клиент игрока.
func TestPacketIDs(t *testing.T) {
	want := map[string][2]int32{
		"login success":       {cbLoginSuccess, 0x02},
		"set compression":     {cbLoginCompression, 0x03},
		"config finish":       {cbConfigFinish, 0x03},
		"registry data":       {cbConfigRegistryData, 0x07},
		"feature flags":       {cbConfigFeatureFlags, 0x0C},
		"known packs (cb)":    {cbConfigKnownPacks, 0x0E},
		"known packs (sb)":    {sbConfigKnownPacks, 0x07},
		"spawn entity":        {cbAddEntity, 0x01},
		"block changed ack":   {cbBlockChangedAck, 0x05},
		"block update":        {cbBlockUpdate, 0x09},
		"chunk batch finish":  {cbChunkBatchFinished, 0x0C},
		"chunk batch start":   {cbChunkBatchStart, 0x0D},
		"disconnect":          {cbDisconnect, 0x1D},
		"unload chunk":        {cbForgetLevelChunk, 0x21},
		"game event":          {cbGameEvent, 0x22},
		"initialize border":   {cbInitializeBorder, 0x25},
		"keep alive (cb)":     {cbKeepAlive, 0x26},
		"chunk with light":    {cbLevelChunkWithLight, 0x27},
		"login (play)":        {cbLogin, 0x2B},
		"move entity pos":     {cbMoveEntityPos, 0x2E},
		"move entity pos rot": {cbMoveEntityPosRot, 0x2F},
		"move entity rot":     {cbMoveEntityRot, 0x30},
		"player abilities":    {cbPlayerAbilities, 0x38},
		"player info remove":  {cbPlayerInfoRemove, 0x3D},
		"player info update":  {cbPlayerInfoUpdate, 0x3E},
		"player position":     {cbPlayerPosition, 0x40},
		"remove entities":     {cbRemoveEntities, 0x42},
		"head rotation":       {cbRotateHead, 0x48},
		"set center chunk":    {cbSetChunkCacheCenter, 0x54},
		"default spawn":       {cbSetDefaultSpawn, 0x56},
		"set time":            {cbSetTime, 0x64},
		"system chat":         {cbSystemChat, 0x6C},
		"teleport entity":     {cbTeleportEntity, 0x70},
		"confirm teleport":    {sbAcceptTeleportation, 0x00},
		"chat command":        {sbChatCommand, 0x04},
		"chat message":        {sbChat, 0x06},
		"keep alive (sb)":     {sbKeepAlive, 0x18},
		"move pos":            {sbMovePlayerPos, 0x1A},
		"move pos rot":        {sbMovePlayerPosRot, 0x1B},
		"move rot":            {sbMovePlayerRot, 0x1C},
		"move status":         {sbMovePlayerStatus, 0x1D},
		"player action":       {sbPlayerAction, 0x24},
		"set held item":       {sbSetCarriedItem, 0x2F},
		"creative slot":       {sbSetCreativeSlot, 0x32},
		"use item on":         {sbUseItemOn, 0x38},
	}
	for name, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s: go-mc id 0x%02X, protocol 767 has 0x%02X", name, v[0], v[1])
		}
	}
	if playerEntityType != 128 {
		t.Errorf("minecraft:player entity type = %d, want 128", playerEntityType)
	}
}

// parseFrame — кадр без сжатия → id и тело.
func parseFrame(t *testing.T, frame []byte) (int32, []byte) {
	t.Helper()
	fr := mcproto.NewFrameReader(bytes.NewReader(frame), mcproto.MaxFrameLen)
	id, p, err := fr.Next()
	if err != nil {
		t.Fatal(err)
	}
	return id, p
}

// TestChunkWithEditsParses — чанк с правками (своя палитра в одной секции
// и прямая палитра в другой) разбирается парсером go-mc в те же блоки.
func TestChunkWithEditsParses(t *testing.T) {
	w := world.New(world.DefaultConfig(), world.Options{}, nil)
	ch := w.Chunks()
	ch.Set(3, 0, 5, world.Stone)
	ch.Set(4, 0, 5, world.Dirt)
	// > 256 разных состояний в секции y=64..79 — прямая палитра.
	n := 0
	for x := int32(0); x < 16; x++ {
		for z := int32(0); z < 16; z++ {
			for y := int32(64); y < 66; y++ {
				n++
				ch.Set(x, y, z, world.State(1000+n))
			}
		}
	}
	enc := NewEncoder(-1)
	pos := world.ChunkPos{X: 0, Z: 0}
	id, body := parseFrame(t, enc.chunkFrame(pos, ch.Column(pos)))
	if id != cbLevelChunkWithLight {
		t.Fatalf("id 0x%x", id)
	}
	r := bytes.NewReader(body)
	var p level.ChunkPos
	c := level.EmptyChunk(world.SectionCount)
	if _, err := p.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 0 {
		t.Fatalf("%d trailing bytes", r.Len())
	}
	get := func(x, y, z int32) block.StateID {
		s := &c.Sections[(y-world.MinY)/16]
		return s.GetBlock(int(((y-world.MinY)%16)<<8 | z<<4 | x))
	}
	if get(3, 0, 5) != block.StateID(world.Stone) || get(4, 0, 5) != block.StateID(world.Dirt) || get(5, 0, 5) != 0 {
		t.Fatal("indirect palette section mismatch")
	}
	if get(0, 64, 0) != 1001 || get(15, 65, 15) != block.StateID(1000+n) {
		t.Fatalf("direct palette section mismatch: %d %d", get(0, 64, 0), get(15, 65, 15))
	}
	if get(1, world.GroundY, 1) != block.StateID(world.GrassBlock) {
		t.Fatal("base section mismatch")
	}
	// Кеш: та же версия — тот же срез; правка — новый пакет.
	f1 := enc.chunkFrame(pos, ch.Column(pos))
	if h, _ := enc.ChunkCacheStats(); h != 1 || &f1[0] != &enc.chunkFrame(pos, ch.Column(pos))[0] {
		t.Fatal("expected cache hit")
	}
	ch.Set(3, 0, 5, world.Air)
	if _, m := enc.ChunkCacheStats(); m != 1 {
		t.Fatal("misses")
	}
	enc.chunkFrame(pos, ch.Column(pos))
	if _, m := enc.ChunkCacheStats(); m != 2 {
		t.Fatal("edit must invalidate cache")
	}
}

func TestMoveEncoding(t *testing.T) {
	enc := NewEncoder(-1)
	p := &world.PlayerInfo{ID: 7}
	near := enc.Append(nil, &world.Event{Kind: world.EvMove, Player: p, From: world.Vec3{X: 0}, To: world.Vec3{X: 1.5}, Moved: true})
	if id, body := parseFrame(t, near); id != cbMoveEntityPos {
		t.Fatalf("near move id 0x%x", id)
	} else {
		d := mcproto.NewDecoder(body)
		if d.VarInt() != 7 || d.Short() != 1.5*4096 {
			t.Fatal("near move body")
		}
	}
	far := enc.Append(nil, &world.Event{Kind: world.EvMove, Player: p, From: world.Vec3{X: 0}, To: world.Vec3{X: 9}, Moved: true})
	if id, _ := parseFrame(t, far); id != cbTeleportEntity {
		t.Fatalf("far move id 0x%x", id)
	}
	rot := enc.Append(nil, &world.Event{Kind: world.EvMove, Player: p, Yaw: 90, Rotated: true})
	if id, _ := parseFrame(t, rot); id != cbMoveEntityRot {
		t.Fatalf("rotation id 0x%x", id)
	}
}

func TestDecodePlay(t *testing.T) {
	in := newInput(&keepAlive{})
	// Предмет камня в слот 36 (панель, ячейка 0), затем установка блока.
	stoneItem := int32(-1)
	for i := int32(1); i < 2000; i++ {
		if s := itemBlock(map[int32]world.State{}, i); s == world.Stone {
			stoneItem = i
			break
		}
	}
	if stoneItem < 0 {
		t.Fatal("no stone item")
	}
	slot := mcproto.AppendShort(nil, 36)
	slot = mcproto.AppendVarInt(slot, 1)
	slot = mcproto.AppendVarInt(slot, stoneItem)
	slot = mcproto.AppendVarInt(slot, 0)
	slot = mcproto.AppendVarInt(slot, 0)
	if _, err := in.decode(sbSetCreativeSlot, slot); err != nil {
		t.Fatal(err)
	}
	use := mcproto.AppendVarInt(nil, 0)
	use = mcproto.AppendPosition(use, 10, world.GroundY, -3)
	use = mcproto.AppendVarInt(use, 1) // верхняя грань
	use = mcproto.AppendFloat(use, 0.5)
	use = mcproto.AppendFloat(use, 1)
	use = mcproto.AppendFloat(use, 0.5)
	use = mcproto.AppendBool(use, false)
	use = mcproto.AppendVarInt(use, 42)
	cmd, err := in.decode(sbUseItemOn, use)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Kind != world.CmdPlace || cmd.Block != (world.BlockPos{X: 10, Y: world.GroundY + 1, Z: -3}) || cmd.State != world.Stone || cmd.Seq != 42 {
		t.Fatalf("place = %+v", cmd)
	}
	// Лишний байт в пакете фиксированного формата — ошибка.
	if _, err := in.decode(sbMovePlayerPos, make([]byte, 26)); err == nil {
		t.Fatal("trailing byte in move must fail")
	}
	if _, err := in.decode(sbAcceptTeleportation, []byte{1, 0}); err == nil {
		t.Fatal("trailing byte in teleport ack must fail")
	}
	// У чата хвост переменный (подпись, подтверждения) — не ошибка.
	if cmd, err := in.decode(sbChat, append(mcproto.AppendString(nil, "hi"), make([]byte, 20)...)); err != nil || cmd.Text != "hi" {
		t.Fatalf("chat with tail: %+v %v", cmd, err)
	}
	// Обрезанный пакет перемещения — ошибка, а не нули.
	if _, err := in.decode(sbMovePlayerPos, make([]byte, 10)); err == nil {
		t.Fatal("short move must fail")
	}
}

func FuzzDecodePlay(f *testing.F) {
	f.Add(int32(sbMovePlayerPos), make([]byte, 25))
	f.Add(int32(sbChat), mcproto.AppendString(nil, "hi"))
	f.Add(int32(sbSetCreativeSlot), []byte{0, 36, 1, 0x80, 0x80, 0x01})
	f.Add(int32(sbUseItemOn), make([]byte, 30))
	f.Fuzz(func(t *testing.T, id int32, payload []byte) {
		in := newInput(&keepAlive{})
		cmd, err := in.decode(id, payload)
		if err == nil && cmd.Kind == world.CmdChat && len(cmd.Text) > 256*4 {
			t.Fatal("chat longer than limit")
		}
	})
}

// --------------------------------------------- интеграция на loopback

type testServer struct {
	addr string
	w    *world.World
}

func startServer(t *testing.T, mod func(*Config)) *testServer {
	t.Helper()
	cfg := DefaultConfig()
	if mod != nil {
		mod(&cfg)
	}
	wcfg := world.DefaultConfig()
	wcfg.ChunksPerTick = 64
	w := world.New(wcfg, world.Options{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, w, NewEncoder(cfg.CompressionThreshold), nopMetrics{}, slog.New(slog.DiscardHandler))
	worldDone := make(chan struct{})
	go func() { w.Run(ctx, "bye"); close(worldDone) }()
	go srv.Serve(ctx, ln)
	t.Cleanup(func() {
		cancel()
		<-worldDone
		wctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		srv.Wait(wctx)
	})
	return &testServer{addr: ln.Addr().String(), w: w}
}

type nopMetrics struct{}

func (nopMetrics) ConnOpened(string)           {}
func (nopMetrics) ConnClosed(string)           {}
func (nopMetrics) ConnRejected(string, string) {}
func (nopMetrics) BytesOut(string, int)        {}
func (nopMetrics) PacketIn(string)             {}

// rawClient — клиент протокола на mcproto (без go-mc): видно каждый байт.
type rawClient struct {
	t    *testing.T
	conn net.Conn
	fr   *mcproto.FrameReader
	pw   *mcproto.PacketWriter
}

func dial(t *testing.T, addr string) *rawClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	t.Cleanup(func() { conn.Close() })
	return &rawClient{t: t, conn: conn, fr: mcproto.NewFrameReader(bufio.NewReader(conn), mcproto.MaxFrameLen), pw: mcproto.NewPacketWriter(-1)}
}

func (c *rawClient) send(body []byte) {
	if _, err := c.conn.Write(c.pw.Frame(nil, body)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *rawClient) handshake(proto int32) {
	b := c.pw.Begin(sbHandshake)
	b = mcproto.AppendVarInt(b, proto)
	b = mcproto.AppendString(b, "localhost")
	b = mcproto.AppendShort(b, 25565)
	b = mcproto.AppendVarInt(b, 2)
	c.send(b)
}

func (c *rawClient) loginStart(name string) {
	b := c.pw.Begin(sbLoginStart)
	b = mcproto.AppendString(b, name)
	b = mcproto.AppendUUID(b, [16]byte{})
	c.send(b)
}

// login проходит вход до configuration (со сжатием).
func (c *rawClient) login(name string) {
	c.handshake(ProtocolVersion)
	c.loginStart(name)
	for {
		id, p, err := c.fr.Next()
		if err != nil {
			c.t.Fatal(err)
		}
		switch id {
		case cbLoginCompression:
			d := mcproto.NewDecoder(p)
			th := int(d.VarInt())
			c.fr.SetCompression(th)
			c.pw.SetThreshold(th)
		case cbLoginSuccess:
			c.send(c.pw.Begin(sbLoginAck))
			return
		default:
			c.t.Fatalf("unexpected login packet 0x%x", id)
		}
	}
}

func TestLoginRejectsWrongVersionAndName(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.addr)
	c.handshake(766)
	c.loginStart("alice")
	id, p, err := c.fr.Next()
	if err != nil || id != cbLoginDisconnect || !strings.Contains(string(p), "1.21.1") {
		t.Fatalf("wrong version: id=0x%x %q %v", id, p, err)
	}
	c = dial(t, s.addr)
	c.handshake(ProtocolVersion)
	c.loginStart("bad name!")
	if id, _, _ := c.fr.Next(); id != cbLoginDisconnect {
		t.Fatalf("bad name: id=0x%x", id)
	}
}

func TestClientWithoutVanillaDataIsRejected(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.addr)
	c.login("nopacks")
	for {
		id, _, err := c.fr.Next()
		if err != nil {
			t.Fatal(err)
		}
		if id == cbConfigKnownPacks {
			b := c.pw.Begin(sbConfigKnownPacks)
			b = mcproto.AppendVarInt(b, 0) // не знаю ни одного набора
			c.send(b)
		}
		if id == cbConfigDisconnect {
			return
		}
	}
}

// joinPlay проходит configuration и возвращает клиента в игре.
func joinPlay(t *testing.T, c *rawClient, name string) {
	t.Helper()
	c.login(name)
	for {
		id, p, err := c.fr.Next()
		if err != nil {
			t.Fatal(err)
		}
		switch id {
		case cbConfigKnownPacks:
			// Отвечаем тем же списком, что предложил сервер.
			b := c.pw.Begin(sbConfigKnownPacks)
			b = append(b, p...)
			c.send(b)
		case cbConfigFinish:
			c.send(c.pw.Begin(sbConfigFinish))
			return
		}
	}
}

func TestPacketFloodIsKicked(t *testing.T) {
	s := startServer(t, func(c *Config) { c.PacketsPerSecond = 10; c.PacketBurst = 20 })
	c := dial(t, s.addr)
	joinPlay(t, c, "flooder")
	var buf []byte
	for i := 0; i < 200; i++ {
		b := c.pw.Begin(sbMovePlayerStatus)
		b = mcproto.AppendBool(b, true)
		buf = c.pw.Frame(buf, b)
	}
	if _, err := c.conn.Write(buf); err != nil {
		t.Fatal(err)
	}
	// Сервер закрывает соединение: дочитываем до EOF/ошибки.
	for {
		if _, _, err := c.fr.Next(); err != nil {
			return
		}
	}
}

func TestOversizedPacketCloses(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.addr)
	joinPlay(t, c, "bigpacket")
	// Кадр на 1 МиБ при пределе 32 КиБ — закрытие по заголовку.
	hdr := mcproto.AppendVarInt(nil, 1<<20)
	if _, err := c.conn.Write(hdr); err != nil {
		t.Fatal(err)
	}
	for {
		if _, _, err := c.fr.Next(); err != nil {
			return
		}
	}
}

func TestPlayJoinSequence(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.addr)
	joinPlay(t, c, "sequence")
	seen := map[int32]bool{}
	first := int32(-1)
	for !seen[cbLevelChunkWithLight] || !seen[cbGameEvent] {
		id, _, err := c.fr.Next()
		if err != nil {
			t.Fatal(err)
		}
		if first < 0 {
			first = id
		}
		seen[id] = true
	}
	if first != cbLogin {
		t.Fatalf("first play packet 0x%x, want Login", first)
	}
	for _, id := range []int32{cbPlayerAbilities, cbPlayerPosition, cbPlayerInfoUpdate, cbSetChunkCacheCenter, cbChunkBatchStart} {
		if !seen[id] {
			t.Errorf("packet 0x%x not received", id)
		}
	}
}

// flakyListener — первые n Accept возвращают ошибку (как EMFILE).
type flakyListener struct {
	net.Listener
	fails atomic.Int32
}

func (l *flakyListener) Accept() (net.Conn, error) {
	if l.fails.Add(-1) >= 0 {
		return nil, errors.New("accept: too many open files")
	}
	return l.Listener.Accept()
}

// TestAcceptErrorsDoNotStopServer — временные ошибки Accept не
// останавливают Serve: пауза с удвоением и повтор.
func TestAcceptErrorsDoNotStopServer(t *testing.T) {
	w := world.New(world.DefaultConfig(), world.Options{}, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fl := &flakyListener{Listener: ln}
	fl.fails.Store(5)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := NewServer(DefaultConfig(), w, NewEncoder(-1), nopMetrics{}, slog.New(slog.DiscardHandler))
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx, fl) }()

	c := dial(t, ln.Addr().String())
	b := c.pw.Begin(sbHandshake)
	b = mcproto.AppendVarInt(b, ProtocolVersion)
	b = mcproto.AppendString(b, "localhost")
	b = mcproto.AppendShort(b, 25565)
	b = mcproto.AppendVarInt(b, 1)
	c.send(b)
	c.send(c.pw.Begin(sbStatusRequest))
	if id, _, err := c.fr.Next(); err != nil || id != cbStatusResponse {
		t.Fatalf("status after accept errors: id=%x err=%v", id, err)
	}
	select {
	case err := <-served:
		t.Fatalf("Serve stopped: %v", err)
	default:
	}
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("Serve after cancel: %v", err)
	}
}

// TestPrePlayPacketLimit — до входа в игру число пакетов ограничено.
func TestPrePlayPacketLimit(t *testing.T) {
	s := startServer(t, nil)
	c := dial(t, s.addr)
	c.login("chatty")
	var buf []byte
	for i := 0; i < 2*maxPrePlayPackets; i++ { // мусор вместо ответа known packs
		buf = c.pw.Frame(buf, append(c.pw.Begin(0x02), 'x'))
	}
	if _, err := c.conn.Write(buf); err != nil {
		t.Fatal(err)
	}
	for {
		if _, _, err := c.fr.Next(); err != nil {
			return // сервер закрыл соединение
		}
	}
}

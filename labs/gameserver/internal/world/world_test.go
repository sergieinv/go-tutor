package world

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// Детерминированная симуляция: мир крутится вручную (Step), часы
// фейковые, клиенты — textProto, который записывает события строками.
// Никаких сокетов, горутин и sleep — тест воспроизводим до байта.

type textProto struct{}

func (textProto) Begin(dst []byte) []byte { return dst }
func (textProto) End(dst []byte, _ *BatchStats) []byte {
	return dst
}

func (textProto) Append(dst []byte, ev *Event) []byte {
	var s string
	switch ev.Kind {
	case EvWelcome:
		s = fmt.Sprintf("welcome %d tp=%d", ev.Player.ID, ev.TeleportID)
	case EvRosterAdd:
		s = fmt.Sprintf("roster+ %d", ev.Player.ID)
	case EvRosterRemove:
		s = fmt.Sprintf("roster- %d", ev.Player.ID)
	case EvSpawn:
		s = fmt.Sprintf("spawn %d", ev.Player.ID)
	case EvDespawn:
		s = fmt.Sprintf("despawn %d", ev.Player.ID)
	case EvMove:
		s = fmt.Sprintf("move %d %.2f %.2f", ev.Player.ID, ev.To.X, ev.To.Z)
	case EvChat:
		s = fmt.Sprintf("chat %d %s", ev.Player.ID, ev.Text)
	case EvSystem:
		s = "system " + ev.Text
	case EvChunkBatchBegin:
		s = "batch"
	case EvChunk:
		s = fmt.Sprintf("chunk %d %d", ev.Chunk.X, ev.Chunk.Z)
	case EvChunkBatchEnd:
		s = fmt.Sprintf("batchend %d", ev.Count)
	case EvUnchunk:
		s = fmt.Sprintf("unchunk %d %d", ev.Chunk.X, ev.Chunk.Z)
	case EvCenter:
		s = fmt.Sprintf("center %d %d", ev.Chunk.X, ev.Chunk.Z)
	case EvBlock:
		s = fmt.Sprintf("block %d %d %d %d", ev.Block.X, ev.Block.Y, ev.Block.Z, ev.State)
	case EvBlockAck:
		s = fmt.Sprintf("ack %d", ev.Seq)
	case EvTeleport:
		s = fmt.Sprintf("teleport %d %s", ev.TeleportID, ev.Reason)
	case EvKick:
		s = "kick " + ev.Text
	}
	return append(append(dst, s...), '\n')
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { c.t = c.t.Add(time.Millisecond); return c.t }

type recMetrics struct {
	NopMetrics
	disconnects map[string]int
	rejected    map[string]int
}

func (m *recMetrics) Disconnect(r string)    { m.disconnects[r]++ }
func (m *recMetrics) RejectedInput(r string) { m.rejected[r]++ }

type harness struct {
	t   *testing.T
	w   *World
	m   *recMetrics
	ctx context.Context
}

func newHarness(t *testing.T, mod func(*Config)) *harness {
	cfg := DefaultConfig()
	cfg.ViewDistance = 2
	cfg.BorderRadius = 256
	cfg.ChunksPerTick = 100
	if mod != nil {
		mod(&cfg)
	}
	m := &recMetrics{disconnects: map[string]int{}, rejected: map[string]int{}}
	clk := &fakeClock{t: time.Unix(0, 0)}
	return &harness{t: t, w: New(cfg, Options{Metrics: m, Now: clk.Now}, nil), m: m, ctx: context.Background()}
}

// client — тестовый клиент: Peer + разбор полученных строк.
type client struct {
	id   EntityID
	peer *Peer
	h    *harness
}

func (h *harness) join(name string) *client {
	h.t.Helper()
	return h.joinQ(name, 1024)
}

func (h *harness) joinQ(name string, queue int) *client {
	h.t.Helper()
	peer := NewPeer(KindMinecraft, textProto{}, queue)
	req := &JoinRequest{Name: name, UUID: [16]byte{1}, Peer: peer, Reply: make(chan JoinResult, 1)}
	h.w.inbox <- Command{Kind: CmdJoin, Join: req}
	h.w.Step()
	r := <-req.Reply
	if r.Err != nil {
		h.t.Fatalf("join %s: %v", name, r.Err)
	}
	c := &client{id: r.ID, peer: peer, h: h}
	return c
}

func (h *harness) send(c Command) { h.w.inbox <- c }

func (h *harness) step(n int) {
	for i := 0; i < n; i++ {
		h.w.Step()
	}
}

// lines забирает всё, что пришло клиенту.
func (c *client) lines() []string {
	var out []string
	for {
		select {
		case b := <-c.peer.Out():
			for _, l := range strings.Split(strings.TrimSuffix(string(b.B), "\n"), "\n") {
				if l != "" {
					out = append(out, l)
				}
			}
			PutBuffer(b)
		default:
			return out
		}
	}
}

func has(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func count(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// ackTP подтверждает начальный телепорт, чтобы перемещения принимались.
func (c *client) ackTP(id int32) {
	c.h.send(Command{Kind: CmdTeleportAck, Player: c.id, TeleportID: id})
}

func (c *client) moveTo(x, z float64) {
	c.h.send(Command{Kind: CmdMove, Player: c.id, Pos: Vec3{x, SpawnY, z}, HasPos: true})
}

func TestJoinOrderAndMutualSpawn(t *testing.T) {
	h := newHarness(t, nil)
	a := h.join("alice")
	la := a.lines()
	if len(la) == 0 || !strings.HasPrefix(la[0], "welcome 1 tp=1") {
		t.Fatalf("first event must be welcome, got %v", la)
	}
	if !has(la, "roster+ 1") || !has(la, "center ") || !has(la, "chunk ") {
		t.Fatalf("missing roster/center/chunks: %v", la)
	}
	// 5×5 чанков (радиус 2), все за один тик (ChunksPerTick=100).
	if n := count(la, "chunk "); n != 25 {
		t.Fatalf("chunks = %d, want 25", n)
	}

	b := h.join("bob")
	la, lb := a.lines(), b.lines()
	if !has(la, "roster+ 2") || !has(la, "spawn 2") || !has(la, "system bob joined") {
		t.Fatalf("alice did not see bob: %v", la)
	}
	if !has(lb, "roster+ 1") || !has(lb, "roster+ 2") || !has(lb, "spawn 1") {
		t.Fatalf("bob did not see alice: %v", lb)
	}
}

func TestDuplicateAndFull(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxPlayers = 1 })
	h.join("alice")
	peer := NewPeer(KindWeb, textProto{}, 8)
	req := &JoinRequest{Name: "alice", Peer: peer, Reply: make(chan JoinResult, 1)}
	h.send(Command{Kind: CmdJoin, Join: req})
	h.step(1)
	if r := <-req.Reply; r.Err != ErrFull { // лимит проверяется первым
		t.Fatalf("err = %v, want ErrFull", r.Err)
	}
}

func TestMoveBroadcastOnlyToViewers(t *testing.T) {
	h := newHarness(t, nil)
	a, b := h.join("alice"), h.join("bob")
	a.ackTP(1)
	b.ackTP(1)
	h.step(1)
	a.lines()
	b.lines()

	a.moveTo(3.5, 3.5)
	h.step(1)
	if lb := b.lines(); !has(lb, "move 1 3.50 3.50") {
		t.Fatalf("bob must get alice's move: %v", lb)
	}
	if la := a.lines(); has(la, "move 1") {
		t.Fatalf("alice must not get her own move: %v", la)
	}
}

func TestWalkOutOfRangeDespawns(t *testing.T) {
	h := newHarness(t, nil)
	a, b := h.join("alice"), h.join("bob")
	a.ackTP(1)
	b.ackTP(1)
	h.step(1)
	a.lines()
	b.lines()

	// Идём на восток по 1 блоку за тик (20 блоков/с < 30) на 120 блоков:
	// радиус видимости — 2 чанка, то есть ~32–48 блоков.
	x := h.w.players[a.id].Pos.X
	var la, lb []string
	for i := 0; i < 120; i++ {
		x++
		a.moveTo(x, 0.5)
		h.step(1)
		la = append(la, a.lines()...)
		lb = append(lb, b.lines()...)
	}
	if !has(lb, "despawn 1") || !has(la, "despawn 2") {
		t.Fatalf("expected mutual despawn\nalice: %v\nbob: %v", la, lb)
	}
	if !has(la, "unchunk ") || !has(la, "center ") {
		t.Fatalf("expected chunk unload and center update")
	}
	// Вернулись — снова видят друг друга.
	for i := 0; i < 120; i++ {
		x--
		a.moveTo(x, 0.5)
		h.step(1)
	}
	if !has(b.lines(), "spawn 1") {
		t.Fatal("bob must see alice again")
	}
}

func TestTooFastIsCorrected(t *testing.T) {
	h := newHarness(t, nil)
	a := h.join("alice")
	a.ackTP(1)
	h.step(1)
	a.lines()
	start := h.w.players[a.id].Pos

	a.moveTo(start.X+100, start.Z) // телепорт на 100 блоков
	h.step(1)
	la := a.lines()
	if !has(la, "teleport 2 too_fast") {
		t.Fatalf("expected correction, got %v", la)
	}
	if got := h.w.players[a.id].Pos; got != start {
		t.Fatalf("position changed to %v", got)
	}
	// До подтверждения перемещения игнорируются.
	a.moveTo(start.X+1, start.Z)
	h.step(1)
	if got := h.w.players[a.id].Pos; got != start {
		t.Fatal("move before teleport ack must be ignored")
	}
	a.ackTP(2)
	a.moveTo(start.X+1, start.Z)
	h.step(1)
	if got := h.w.players[a.id].Pos; got.X != start.X+1 {
		t.Fatalf("move after ack must be accepted, pos %v", got)
	}
	if h.m.rejected[rejectTooFast] != 1 {
		t.Fatalf("rejected = %v", h.m.rejected)
	}
}

func TestNaNKicks(t *testing.T) {
	h := newHarness(t, nil)
	a, b := h.join("alice"), h.join("bob")
	b.lines()
	h.send(Command{Kind: CmdMove, Player: a.id, Pos: Vec3{math.NaN(), 0, 0}, HasPos: true})
	h.step(1)
	if a.peer.Reason() != ReasonBadInput {
		t.Fatalf("reason = %q", a.peer.Reason())
	}
	if la := a.lines(); !has(la, "kick Invalid position") {
		t.Fatalf("kicked player must get the reason: %v", la)
	}
	lb := b.lines()
	if !has(lb, "despawn 1") || !has(lb, "roster- 1") {
		t.Fatalf("bob must see alice leave: %v", lb)
	}
	if h.m.disconnects[ReasonBadInput] != 1 {
		t.Fatal("disconnect not counted")
	}
}

func TestChatInterestAndLimits(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ChatBurst = 2 })
	a, b := h.join("alice"), h.join("bob")
	far := h.join("carol")
	far.ackTP(1)
	h.step(1)
	// Уводим carol далеко (по 1 блоку за тик).
	x := h.w.players[far.id].Pos.X
	for i := 0; i < 150; i++ {
		x++
		far.moveTo(x, 0.5)
		h.step(1)
	}
	a.lines()
	b.lines()
	far.lines()

	h.send(Command{Kind: CmdChat, Player: a.id, Text: "  привет  "})
	h.step(1)
	if la := a.lines(); !has(la, "chat 1 привет") {
		t.Fatalf("author must get own message: %v", la)
	}
	if lb := b.lines(); !has(lb, "chat 1 привет") {
		t.Fatalf("viewer must get message: %v", lb)
	}
	if lf := far.lines(); has(lf, "chat ") {
		t.Fatalf("far player must not get message: %v", lf)
	}

	h.send(Command{Kind: CmdChat, Player: a.id, Text: "bad\x07bell"})
	h.send(Command{Kind: CmdChat, Player: a.id, Text: "two"})
	h.send(Command{Kind: CmdChat, Player: a.id, Text: "three"}) // сверх лимита (2 жетона)
	h.step(1)
	la := a.lines()
	if !has(la, "system Message rejected") || !has(la, "system Too many") {
		t.Fatalf("expected rejections: %v", la)
	}
}

func TestSlowConsumerIsDisconnected(t *testing.T) {
	h := newHarness(t, nil)
	slow := h.joinQ("slow", 2) // очередь на 2 пачки, никто не читает
	b := h.join("bob")
	b.ackTP(1)
	for i := 0; i < 5; i++ {
		b.moveTo(h.w.players[b.id].Pos.X+0.5, 0.5)
		h.step(1)
	}
	if slow.peer.Reason() != ReasonSlowConsumer {
		t.Fatalf("reason = %q", slow.peer.Reason())
	}
	if _, ok := h.w.players[slow.id]; ok {
		t.Fatal("slow player must be removed")
	}
	if !has(b.lines(), "roster- 1") {
		t.Fatal("bob must see slow leave")
	}
}

func TestBlocks(t *testing.T) {
	h := newHarness(t, nil)
	a := h.join("alice")
	a.ackTP(1)
	h.step(1)
	a.lines()
	p := h.w.players[a.id].Pos
	bx, bz := int32(math.Floor(p.X))+1, int32(math.Floor(p.Z))

	h.send(Command{Kind: CmdPlace, Player: a.id, Block: BlockPos{bx, SpawnY, bz}, State: Stone, Seq: 7})
	h.step(1)
	la := a.lines()
	if !has(la, fmt.Sprintf("block %d %d %d %d", bx, SpawnY, bz, Stone)) || !has(la, "ack 7") {
		t.Fatalf("place: %v", la)
	}
	if h.w.chunks.Block(bx, SpawnY, bz) != Stone {
		t.Fatal("block not placed")
	}
	// Далеко — отказ, но подтверждение и настоящее состояние приходят.
	h.send(Command{Kind: CmdDig, Player: a.id, Block: BlockPos{bx + 20, GroundY, bz}, Seq: 8})
	h.step(1)
	la = a.lines()
	if !has(la, "ack 8") || h.m.rejected[rejectBlockReach] != 1 {
		t.Fatalf("far dig must be rejected: %v %v", la, h.m.rejected)
	}
	// Бедрок не ломается (пол мира).
	h.send(Command{Kind: CmdDig, Player: a.id, Block: BlockPos{bx, MinY, bz}, Seq: 9})
	h.step(1)
	if h.w.chunks.Block(bx, MinY, bz) != Bedrock {
		t.Fatal("bedrock must stay")
	}
	// Снапшот и восстановление.
	snap := h.w.Snapshot()
	w2 := New(h.w.cfg, Options{}, snap)
	if w2.chunks.Block(bx, SpawnY, bz) != Stone {
		t.Fatal("restored world lost the block")
	}
	if len(snap.Players) != 1 || snap.Players[0].Name != "alice" {
		t.Fatalf("players in snapshot: %v", snap.Players)
	}
}

func TestSavedPositionOnRejoin(t *testing.T) {
	h := newHarness(t, nil)
	a := h.join("alice")
	a.ackTP(1)
	h.step(1)
	p := h.w.players[a.id].Pos
	a.moveTo(p.X+5, p.Z)
	h.step(1)
	h.send(Command{Kind: CmdLeave, Player: a.id, Reason: ReasonQuit})
	h.step(1)
	a2 := h.join("alice")
	if got := h.w.players[a2.id].Pos.X; got != p.X+5 {
		t.Fatalf("rejoin x = %v, want %v", got, p.X+5)
	}
}

func TestShutdownKicksAll(t *testing.T) {
	h := newHarness(t, nil)
	a := h.join("alice")
	a.lines()
	snap := h.w.Shutdown("bye")
	if !has(a.lines(), "kick bye") || a.peer.Reason() != ReasonShutdown {
		t.Fatal("expected kick on shutdown")
	}
	if len(snap.Players) != 1 {
		t.Fatalf("snapshot players: %v", snap.Players)
	}
}

func TestSchedule(t *testing.T) {
	base := time.Unix(100, 0)
	// Вовремя: расписание не меняется.
	if n, s := schedule(base, base.Add(-10*time.Millisecond)); !n.Equal(base) || s != 0 {
		t.Fatal("on time")
	}
	// Небольшое отставание — догоняем без пропуска.
	if n, s := schedule(base, base.Add(3*TickInterval)); !n.Equal(base) || s != 0 {
		t.Fatal("small lag must be caught up")
	}
	// Большое отставание (секунда) — долг списан, 20 тиков пропущено.
	now := base.Add(time.Second)
	if n, s := schedule(base, now); !n.Equal(now) || s != 20 {
		t.Fatalf("big lag: next=%v skipped=%d", n, s)
	}
}

// TestVisibilityInvariant — свойство interest management на случайных
// блужданиях: клиент, собирающий своё представление только из событий
// spawn/despawn, в каждый момент видит ровно тех, кто в радиусе.
func TestVisibilityInvariant(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxSpeed = 1000; c.BorderRadius = 200 })
	rng := rand.New(rand.NewPCG(42, 42))
	const n = 12
	cl := make([]*client, n)
	views := make([]map[string]bool, n)
	for i := range cl {
		cl[i] = h.join(fmt.Sprintf("p%02d", i))
		cl[i].ackTP(1)
		views[i] = map[string]bool{}
	}
	h.step(1)
	for tick := 0; tick < 300; tick++ {
		for i, c := range cl {
			if rng.IntN(3) == 0 {
				continue
			}
			p := h.w.players[c.id].Pos
			x := min(max(p.X+float64(rng.IntN(31)-15), -190), 190)
			z := min(max(p.Z+float64(rng.IntN(31)-15), -190), 190)
			cl[i].moveTo(x, z)
		}
		h.step(1)
		for i, c := range cl {
			for _, l := range c.lines() {
				f := strings.Fields(l)
				switch f[0] {
				case "spawn":
					if views[i][f[1]] {
						t.Fatalf("tick %d: %s spawned twice for %d", tick, f[1], i)
					}
					views[i][f[1]] = true
				case "despawn":
					if !views[i][f[1]] {
						t.Fatalf("tick %d: despawn of unseen %s for %d", tick, f[1], i)
					}
					delete(views[i], f[1])
				case "move":
					if !views[i][f[1]] {
						t.Fatalf("tick %d: move of unseen %s for %d", tick, f[1], i)
					}
				}
			}
		}
		for i, c := range cl {
			me := h.w.players[c.id]
			for j, o := range cl {
				if i == j {
					continue
				}
				other := h.w.players[o.id]
				want := chebyshev(ChunkOf(me.Pos), ChunkOf(other.Pos)) <= int32(h.w.cfg.ViewDistance)
				if got := views[i][fmt.Sprint(o.id)]; got != want {
					t.Fatalf("tick %d: %d sees %d = %v, want %v", tick, i, j, got, want)
				}
			}
		}
	}
}

// TestStepAllocsSteadyState — тик без событий не аллоцирует:
// буферы пачек из пула, исходящая очередь событий переиспользуется.
func TestStepAllocsSteadyState(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	h := newHarness(t, nil)
	cl := make([]*client, 20)
	for i := range cl {
		cl[i] = h.join(fmt.Sprintf("p%02d", i))
		cl[i].ackTP(1)
	}
	h.step(2)
	for _, c := range cl {
		c.lines()
	}
	allocs := testing.AllocsPerRun(100, func() {
		h.w.Step()
		for _, c := range cl {
			c.lines()
		}
	})
	if allocs > 0 {
		t.Fatalf("idle tick allocates %.1f times", allocs)
	}
}

func TestBlockEditLimit(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxEdits = 2; c.BlockBurst = 100 })
	a := h.join("alice")
	a.ackTP(1)
	h.step(1)
	p := h.w.players[a.id].Pos
	bx, bz := int32(math.Floor(p.X)), int32(math.Floor(p.Z))
	for i := int32(0); i < 3; i++ {
		h.send(Command{Kind: CmdPlace, Player: a.id, Block: BlockPos{bx + 1 + i, SpawnY, bz}, State: Stone, Seq: i})
	}
	h.step(1)
	if h.w.chunks.Edits() != 2 || h.m.rejected[rejectBlockLimit] != 1 {
		t.Fatalf("edits = %d, rejected = %v", h.w.chunks.Edits(), h.m.rejected)
	}
	if !has(a.lines(), "system World edit limit") {
		t.Fatal("player must be told about the limit")
	}
	// Вернуть блок как было можно и на пределе: правок становится меньше.
	h.send(Command{Kind: CmdDig, Player: a.id, Block: BlockPos{bx + 1, SpawnY, bz}, Seq: 9})
	h.step(1)
	if h.w.chunks.Edits() != 1 {
		t.Fatalf("dig at limit: edits = %d", h.w.chunks.Edits())
	}
}

type busySink struct{ skips, offers int }

func (s *busySink) Busy() bool           { return true }
func (s *busySink) Skip()                { s.skips++ }
func (s *busySink) Offer(*Snapshot) bool { s.offers++; return true }

// TestSnapshotSkippedWhenBusy — сохранение занято: тик не делает копию и
// не зовёт Offer, но пропуск учитывается (Skip → метрика skipped).
func TestSnapshotSkippedWhenBusy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SaveEveryTicks = 1
	sink := &busySink{}
	w := New(cfg, Options{Snapshots: sink}, nil)
	for i := 0; i < 3; i++ {
		w.Step()
	}
	if sink.skips != 3 || sink.offers != 0 {
		t.Fatalf("skips=%d offers=%d", sink.skips, sink.offers)
	}
}

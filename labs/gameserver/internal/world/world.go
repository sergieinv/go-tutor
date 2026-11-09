package world

import (
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"
	"sync/atomic"
	"time"
)

// Player — игрок в мире. Принадлежит горутине тика.
type Player struct {
	PlayerInfo
	peer *Peer

	// Разосланное состояние: от него считаются дельты перемещения.
	sentPos    Vec3
	sentYaw    float32
	sentPitch  float32
	onGround   bool
	dirtyPos   bool
	dirtyRot   bool
	moveBudget float64 // сколько блоков игрок ещё может пройти (проверка скорости)
	budgetTick uint64

	// Поправка позиции: пока клиент не подтвердил телепорт, его
	// перемещения игнорируются (как в ванильном сервере).
	pendingTP  int32
	nextTP     int32
	tpSentTick uint64

	// Interest management.
	center   ChunkPos           // чанк игрока — центр зоны видимости
	loaded   map[ChunkPos]bool  // чанки, отправленные клиенту
	pending  []ChunkPos         // чанки к отправке, ближние первыми
	visible  map[*Player]uint64 // кого видит → тик, когда увидел
	visDirty bool               // пересчитать видимость в фазе обновления

	chat   bucket
	blocks bucket

	batch  *Buffer
	events int
	gone   bool
}

// audience — кому адресовано событие исходящей очереди тика.
type audience uint8

const (
	toOne     audience = iota // одному игроку (target)
	toAll                     // всем, кроме exclude
	toViewers                 // тем, кто видит Player события (+ сам, если self)
	toChunk                   // тем, у кого загружен чанк события
)

type outItem struct {
	ev      Event
	aud     audience
	target  *Player
	exclude *Player
	self    bool
}

// World — состояние мира и игровой цикл.
type World struct {
	cfg     Config
	log     *slog.Logger
	metrics Metrics
	now     func() time.Time
	snaps   SnapshotSink

	inbox   chan Command
	stopped chan struct{}

	tick    uint64
	nextID  EntityID
	players map[EntityID]*Player
	byName  map[string]*Player
	list    []*Player // порядок входа: детерминированный обход
	offline map[string]PlayerState
	grid    *grid
	chunks  *Chunks
	rng     *rand.Rand
	spawn   Vec3

	welcome  Welcome
	out      []outItem
	kicks    []*Player
	cache    []encCache
	stats    BatchStats
	endStats BatchStats
	moveEv   Event

	// Измерения цикла (пишет Run, читают кодировщики через stats).
	lastMSPT    float64
	tps         float64
	tickStarts  [TickRate + 1]time.Time
	tickStartsN int
	lastWarn    time.Time

	online atomic.Int32 // для пинга списка серверов из других горутин
}

type encCache struct {
	proto Protocol
	buf   []byte
	valid bool
}

// New создаёт мир и восстанавливает его из снапшота (snap может быть nil).
func New(cfg Config, opts Options, snap *Snapshot) *World {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	if opts.Metrics == nil {
		opts.Metrics = NopMetrics{}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	w := &World{
		cfg:     cfg,
		log:     opts.Log,
		metrics: opts.Metrics,
		now:     opts.Now,
		snaps:   opts.Snapshots,
		inbox:   make(chan Command, cfg.InboxSize),
		stopped: make(chan struct{}),
		nextID:  1,
		players: make(map[EntityID]*Player),
		byName:  make(map[string]*Player),
		offline: make(map[string]PlayerState),
		grid:    newGrid(),
		chunks:  newChunks(),
		rng:     rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9E3779B97F4A7C15)),
		spawn:   Vec3{0.5, SpawnY, 0.5},
	}
	w.welcome = Welcome{
		ViewDistance: cfg.ViewDistance, MaxPlayers: cfg.MaxPlayers,
		BorderRadius: cfg.BorderRadius, Spawn: w.spawn, TickRate: TickRate,
		Placeable: PlaceableNames(), MaxChat: cfg.MaxChat,
	}
	if snap != nil {
		w.restore(snap)
	}
	return w
}

func (w *World) restore(s *Snapshot) {
	w.tick = s.Tick
	n := 0
	for _, b := range s.Blocks {
		if b.Y <= MinY || b.Y > MaxY || int(b.State) >= StateCount() ||
			math.Abs(float64(b.X)) > w.cfg.BorderRadius || math.Abs(float64(b.Z)) > w.cfg.BorderRadius {
			continue
		}
		if w.cfg.MaxEdits > 0 && w.chunks.Edits() >= w.cfg.MaxEdits {
			w.log.Warn("snapshot has more block edits than the limit, rest ignored", "limit", w.cfg.MaxEdits)
			break
		}
		if w.chunks.Set(b.X, b.Y, b.Z, b.State) {
			n++
		}
	}
	for _, p := range s.Players {
		if validName(p.Name) && w.inBounds(Vec3{p.X, p.Y, p.Z}) {
			w.offline[p.Name] = p
		}
	}
	w.log.Info("world restored", "tick", s.Tick, "blocks", n, "players", len(w.offline))
}

// Tick — номер текущего тика.
func (w *World) Tick() uint64 { return w.tick }

// Online — число игроков (безопасно из любой горутины).
func (w *World) Online() int { return int(w.online.Load()) }

// MaxPlayers — лимит игроков (конфигурация неизменна, безопасно из любой горутины).
func (w *World) MaxPlayers() int { return w.cfg.MaxPlayers }

// Chunks — блоки мира (только для горутины тика и тестов).
func (w *World) Chunks() *Chunks { return w.chunks }

// Step выполняет один тик: ввод → обновление → рассылка. Вызывается
// только из одной горутины (Run или тест).
func (w *World) Step() {
	start := w.now()
	w.tick++
	w.stats = BatchStats{Tick: w.tick, MSPT: w.lastMSPT, TPS: w.tps}

	w.input()
	w.update()
	w.broadcast()

	if w.cfg.SaveEveryTicks > 0 && w.tick%uint64(w.cfg.SaveEveryTicks) == 0 && w.snaps != nil {
		// Прошлое сохранение ещё идёт — не копируем состояние зря.
		if w.snaps.Busy() {
			w.snaps.Skip() // учесть пропуск в метрике, копию не делаем
			w.log.Warn("snapshot skipped: previous save is still running")
		} else if !w.snaps.Offer(w.Snapshot()) {
			w.log.Warn("snapshot skipped: previous save is still running")
		}
	}

	d := w.now().Sub(start)
	w.lastMSPT = float64(d) / float64(time.Millisecond)
	w.metrics.TickDone(d)
}

// ---------------------------------------------------------------- ввод

// input разбирает накопившиеся команды — не больше MaxCommandsPerTick,
// чтобы поток ввода не растягивал тик. Остаток подождёт в канале.
func (w *World) input() {
	for _, p := range w.kicks { // отключённые в прошлом тике (медленные)
		w.remove(p, ReasonSlowConsumer)
	}
	w.kicks = w.kicks[:0]

	w.metrics.InboxLen(len(w.inbox))
	for i := 0; i < w.cfg.MaxCommandsPerTick; i++ {
		select {
		case c := <-w.inbox:
			w.apply(&c)
		default:
			return
		}
	}
}

func (w *World) apply(c *Command) {
	if c.Kind == CmdJoin {
		w.join(c.Join)
		return
	}
	p := w.players[c.Player]
	if p == nil {
		return // уже отключён (например, медленный клиент) — команда опоздала
	}
	switch c.Kind {
	case CmdLeave, CmdKick:
		w.remove(p, c.Reason)
	case CmdMove:
		w.move(p, c)
	case CmdTeleportAck:
		if p.pendingTP != 0 && c.TeleportID == p.pendingTP {
			p.pendingTP = 0
		}
	case CmdChat:
		w.chat(p, c.Text)
	case CmdDig:
		w.dig(p, c.Block, c.Seq)
	case CmdPlace:
		w.place(p, c.Block, c.State, c.Seq)
	}
}

func (w *World) emit(it outItem) { w.out = append(w.out, it) }

func (w *World) join(r *JoinRequest) {
	reply := func(id EntityID, err error) { r.Reply <- JoinResult{ID: id, Err: err} }
	switch {
	case w.cfg.MaxPlayers > 0 && len(w.players) >= w.cfg.MaxPlayers:
		w.metrics.JoinRejected("full")
		reply(0, ErrFull)
		return
	case w.byName[r.Name] != nil:
		w.metrics.JoinRejected(ReasonDuplicate)
		reply(0, ErrDuplicate)
		return
	}
	pos, yaw, pitch := w.spawnPoint(r.Name)
	p := &Player{
		PlayerInfo: PlayerInfo{ID: w.nextID, UUID: r.UUID, Name: r.Name, Kind: r.Peer.Kind(), Pos: pos, Yaw: yaw, Pitch: pitch},
		peer:       r.Peer,
		sentPos:    pos, sentYaw: yaw, sentPitch: pitch,
		moveBudget: w.cfg.MaxSpeed,
		budgetTick: w.tick,
		center:     ChunkOf(pos),
		loaded:     make(map[ChunkPos]bool),
		visible:    make(map[*Player]uint64),
		visDirty:   true,
		chat:       newBucket(w.tick, w.cfg.ChatBurst),
		blocks:     newBucket(w.tick, w.cfg.BlockBurst),
	}
	w.nextID++
	// Первая позиция приходит клиенту как телепорт: пока он её не
	// подтвердит, его перемещения не принимаются.
	p.nextTP = 1
	p.pendingTP = 1
	p.tpSentTick = w.tick
	w.players[p.ID] = p
	w.byName[p.Name] = p
	w.list = append(w.list, p)
	w.grid.add(p, p.center)
	delete(w.offline, p.Name)
	w.countPlayers()
	reply(p.ID, nil)

	w.emit(outItem{ev: Event{Kind: EvWelcome, Player: &p.PlayerInfo, TeleportID: p.pendingTP, Welcome: &w.welcome}, aud: toOne, target: p})
	for _, q := range w.list { // список игроков — новому, включая его самого
		w.emit(outItem{ev: Event{Kind: EvRosterAdd, Player: &q.PlayerInfo}, aud: toOne, target: p})
	}
	w.emit(outItem{ev: Event{Kind: EvRosterAdd, Player: &p.PlayerInfo}, aud: toAll, exclude: p})
	w.emit(outItem{ev: Event{Kind: EvCenter, Chunk: p.center}, aud: toOne, target: p})
	w.planChunks(p)
	w.emit(outItem{ev: Event{Kind: EvSystem, Text: p.Name + " joined the game"}, aud: toAll})
	w.log.Info("player joined", "id", p.ID, "name", p.Name, "kind", p.Kind.String(), "online", len(w.players))
}

// spawnPoint — сохранённая позиция или точка появления с разбросом
// (детерминированным: зерно в конфиге).
func (w *World) spawnPoint(name string) (Vec3, float32, float32) {
	if s, ok := w.offline[name]; ok {
		return Vec3{s.X, s.Y, s.Z}, s.Yaw, s.Pitch
	}
	return Vec3{
		X: w.spawn.X + float64(w.rng.IntN(17)-8),
		Y: w.spawn.Y,
		Z: w.spawn.Z + float64(w.rng.IntN(17)-8),
	}, 0, 0
}

func (w *World) countPlayers() {
	var mc, web int
	for _, p := range w.list {
		if p.Kind == KindWeb {
			web++
		} else {
			mc++
		}
	}
	w.metrics.Players(KindMinecraft, mc)
	w.metrics.Players(KindWeb, web)
	w.online.Store(int32(len(w.list)))
}

// remove убирает игрока из мира: из сетки, из зон видимости остальных, из
// списка. Соединение закрывается через Peer.
func (w *World) remove(p *Player, reason string) {
	if p.gone {
		return
	}
	p.gone = true
	delete(w.players, p.ID)
	delete(w.byName, p.Name)
	if i := slices.Index(w.list, p); i >= 0 {
		w.list = slices.Delete(w.list, i, i+1)
	}
	w.grid.remove(p, p.center)
	for q := range p.visible {
		delete(q.visible, p)
		w.emit(outItem{ev: Event{Kind: EvDespawn, Player: &p.PlayerInfo}, aud: toOne, target: q})
	}
	clear(p.visible)
	w.rememberOffline(p)
	w.emit(outItem{ev: Event{Kind: EvRosterRemove, Player: &p.PlayerInfo}, aud: toAll})
	w.emit(outItem{ev: Event{Kind: EvSystem, Text: p.Name + " left the game"}, aud: toAll})
	w.metrics.Disconnect(reason)
	w.countPlayers()
	p.peer.Close(reason)
	w.log.Info("player left", "id", p.ID, "name", p.Name, "reason", reason, "online", len(w.players))
}

func (w *World) rememberOffline(p *Player) {
	if _, ok := w.offline[p.Name]; !ok && len(w.offline) >= w.cfg.MaxOffline {
		for k := range w.offline { // вытесняем произвольную запись
			delete(w.offline, k)
			break
		}
	}
	w.offline[p.Name] = PlayerState{Name: p.Name, X: p.Pos.X, Y: p.Pos.Y, Z: p.Pos.Z, Yaw: p.Yaw, Pitch: p.Pitch}
}

// ---------------------------------------------------------- обновление

// update пересчитывает то, что зависит от перемещений: центр зоны
// видимости, загрузку и выгрузку чанков, кто кого видит; затем ставит в
// очередь порцию чанков каждому игроку.
func (w *World) update() {
	vd := int32(w.cfg.ViewDistance)
	for _, p := range w.list {
		if c := ChunkOf(p.Pos); c != p.center {
			w.grid.move(p, p.center, c)
			p.center = c
			p.visDirty = true
			for ch := range p.loaded { // вышедшие из квадрата — выгрузить
				if chebyshev(ch, c) > vd {
					delete(p.loaded, ch)
					w.emit(outItem{ev: Event{Kind: EvUnchunk, Chunk: ch}, aud: toOne, target: p})
				}
			}
			w.emit(outItem{ev: Event{Kind: EvCenter, Chunk: c}, aud: toOne, target: p})
			w.planChunks(p)
		}
		// Клиент не подтвердил телепорт за 2 с — повторить.
		if p.pendingTP != 0 && w.tick-p.tpSentTick >= 2*TickRate {
			w.teleport(p, "resend")
		}
	}
	// Видимость — после того как центры всех игроков обновлены. Отношение
	// симметрично (у всех один радиус), поэтому достаточно пересчитать
	// только тех, кто сменил чанк: пары с ними обновятся с обеих сторон.
	for _, p := range w.list {
		if p.visDirty {
			w.updateVisibility(p)
			p.visDirty = false
		}
	}
	for _, p := range w.list {
		w.streamChunks(p)
	}
}

func (w *World) updateVisibility(p *Player) {
	vd := int32(w.cfg.ViewDistance)
	w.grid.near(p.center, vd, func(q *Player) {
		if q == p {
			return
		}
		if _, ok := p.visible[q]; ok {
			return
		}
		p.visible[q] = w.tick
		q.visible[p] = w.tick
		w.emit(outItem{ev: Event{Kind: EvSpawn, Player: &q.PlayerInfo}, aud: toOne, target: p})
		w.emit(outItem{ev: Event{Kind: EvSpawn, Player: &p.PlayerInfo}, aud: toOne, target: q})
	})
	for q := range p.visible {
		if chebyshev(p.center, q.center) > vd {
			delete(p.visible, q)
			delete(q.visible, p)
			w.emit(outItem{ev: Event{Kind: EvDespawn, Player: &q.PlayerInfo}, aud: toOne, target: p})
			w.emit(outItem{ev: Event{Kind: EvDespawn, Player: &p.PlayerInfo}, aud: toOne, target: q})
		}
	}
}

// planChunks строит очередь недостающих чанков вокруг центра, ближние
// первыми: клиент быстрее видит землю под ногами.
func (w *World) planChunks(p *Player) {
	vd := int32(w.cfg.ViewDistance)
	c := p.center
	p.pending = p.pending[:0]
	for x := c.X - vd; x <= c.X+vd; x++ {
		for z := c.Z - vd; z <= c.Z+vd; z++ {
			ch := ChunkPos{x, z}
			if !p.loaded[ch] && w.chunkInWorld(ch) {
				p.pending = append(p.pending, ch)
			}
		}
	}
	slices.SortFunc(p.pending, func(a, b ChunkPos) int {
		da, db := dist2(a, c), dist2(b, c)
		if da != db {
			return int(da - db)
		}
		if a.X != b.X {
			return int(a.X - b.X)
		}
		return int(a.Z - b.Z)
	})
}

func dist2(a, b ChunkPos) int32 {
	dx, dz := a.X-b.X, a.Z-b.Z
	return dx*dx + dz*dz
}

// chunkInWorld — чанк пересекает квадрат границы мира (за ней — пустота,
// чанки не отправляем).
func (w *World) chunkInWorld(c ChunkPos) bool {
	r := int32(math.Ceil(w.cfg.BorderRadius))
	return c.X*16+15 >= -r && c.X*16 <= r && c.Z*16+15 >= -r && c.Z*16 <= r
}

func (w *World) streamChunks(p *Player) {
	n := min(w.cfg.ChunksPerTick, len(p.pending))
	if n == 0 {
		return
	}
	w.emit(outItem{ev: Event{Kind: EvChunkBatchBegin}, aud: toOne, target: p})
	for _, ch := range p.pending[:n] {
		p.loaded[ch] = true
		w.emit(outItem{ev: Event{Kind: EvChunk, Chunk: ch, Column: w.chunks.Column(ch)}, aud: toOne, target: p})
	}
	w.emit(outItem{ev: Event{Kind: EvChunkBatchEnd, Count: n}, aud: toOne, target: p})
	p.pending = append(p.pending[:0], p.pending[n:]...)
	w.metrics.ChunksSent(n)
}

// ------------------------------------------------------------- рассылка

// broadcast кодирует события тика в пачки игроков и отправляет пачки в
// очереди соединений. Широковещательное событие кодируется один раз на
// протокол, а не на получателя.
func (w *World) broadcast() {
	for _, p := range w.list {
		p.batch = GetBuffer()
		p.batch.B = p.peer.proto.Begin(p.batch.B)
		p.events = 0
	}
	for i := range w.out {
		w.deliver(&w.out[i])
	}
	clear(w.out) // не держим ссылки на ушедших игроков
	w.out = w.out[:0]

	// Перемещения: каждому, кто видел игрока до этого тика (новые
	// зрители получили актуальную позицию в событии появления).
	ev := &w.moveEv // поле, а не локальная переменная: &ev в интерфейсный метод «убежал» бы в кучу
	for _, p := range w.list {
		if !p.dirtyPos && !p.dirtyRot {
			continue
		}
		*ev = Event{
			Kind: EvMove, Player: &p.PlayerInfo, From: p.sentPos, To: p.Pos,
			Yaw: p.Yaw, Pitch: p.Pitch, Moved: p.dirtyPos, Rotated: p.dirtyRot, OnGround: p.onGround,
		}
		w.resetCache()
		for q, since := range p.visible {
			if since != w.tick {
				w.appendEncoded(q, ev)
			}
		}
		p.sentPos, p.sentYaw, p.sentPitch = p.Pos, p.Yaw, p.Pitch
		p.dirtyPos, p.dirtyRot = false, false
	}

	// Завершаем пачки и отправляем не блокируясь.
	for _, p := range w.list {
		// Статистика — поле мира, а не локальная переменная: указатель,
		// переданный в метод интерфейса, иначе «убежал» бы в кучу.
		w.endStats = w.stats
		w.endStats.Events = p.events
		p.batch.B = p.peer.proto.End(p.batch.B, &w.endStats)
		b := p.batch
		p.batch = nil
		if len(b.B) == 0 {
			PutBuffer(b)
			continue
		}
		if !p.peer.trySend(b) {
			PutBuffer(b)
			// Очередь полна: клиент не успевает читать. Тик не ждёт —
			// отключаем, убираем из мира в начале следующего тика.
			w.log.Warn("slow consumer disconnected", "id", p.ID, "name", p.Name, "queue", cap(p.peer.out))
			p.peer.Close(ReasonSlowConsumer)
			w.kicks = append(w.kicks, p)
		}
	}
}

func (w *World) resetCache() {
	for i := range w.cache {
		w.cache[i].valid = false
	}
}

// appendEncoded дописывает событие в пачку q, кодируя его не больше
// одного раза на протокол.
func (w *World) appendEncoded(q *Player, ev *Event) {
	if q.gone || q.batch == nil {
		return
	}
	proto := q.peer.proto
	var c *encCache
	for i := range w.cache {
		if w.cache[i].proto == proto {
			c = &w.cache[i]
			break
		}
	}
	if c == nil {
		w.cache = append(w.cache, encCache{proto: proto})
		c = &w.cache[len(w.cache)-1]
	}
	if !c.valid {
		c.buf = proto.Append(c.buf[:0], ev)
		c.valid = true
	}
	if len(c.buf) > 0 {
		q.batch.B = append(q.batch.B, c.buf...)
		q.events++
	}
}

func (w *World) appendDirect(q *Player, ev *Event) {
	if q.gone || q.batch == nil {
		return
	}
	n := len(q.batch.B)
	q.batch.B = q.peer.proto.Append(q.batch.B, ev)
	if len(q.batch.B) > n {
		q.events++
	}
}

func (w *World) deliver(it *outItem) {
	switch it.aud {
	case toOne:
		w.appendDirect(it.target, &it.ev)
	case toAll:
		w.resetCache()
		for _, q := range w.list {
			if q != it.exclude {
				w.appendEncoded(q, &it.ev)
			}
		}
	case toViewers:
		w.resetCache()
		src := w.players[it.ev.Player.ID]
		if src == nil {
			return
		}
		if it.self {
			w.appendEncoded(src, &it.ev)
		}
		for q := range src.visible {
			w.appendEncoded(q, &it.ev)
		}
	case toChunk:
		w.resetCache()
		ch := ChunkPos{it.ev.Block.X >> 4, it.ev.Block.Z >> 4}
		for _, q := range w.list {
			if q.loaded[ch] {
				w.appendEncoded(q, &it.ev)
			}
		}
	}
}

// ------------------------------------------------------------ снапшот

// Snapshot копирует сохраняемое состояние. Копия — чтобы горутина
// сохранения писала файл, не мешая тику и не гоняясь с ним за данными.
// В тике — только копирование (O(правок), их число ограничено MaxEdits);
// сортировку для стабильного файла делает горутина сохранения (Sort).
func (w *World) Snapshot() *Snapshot {
	s := &Snapshot{Tick: w.tick}
	s.Players = make([]PlayerState, 0, len(w.offline)+len(w.list))
	for _, st := range w.offline {
		s.Players = append(s.Players, st)
	}
	for _, p := range w.list {
		s.Players = append(s.Players, PlayerState{Name: p.Name, X: p.Pos.X, Y: p.Pos.Y, Z: p.Pos.Z, Yaw: p.Yaw, Pitch: p.Pitch})
	}
	s.Blocks = w.chunks.appendEdits(nil)
	return s
}

// Shutdown отключает всех игроков с причиной и возвращает финальный
// снапшот. Вызывается из горутины тика (Run) после остановки цикла.
func (w *World) Shutdown(text string) *Snapshot {
	for _, p := range slices.Clone(w.list) {
		w.kick(p, text, ReasonShutdown)
	}
	w.out = w.out[:0] // события ухода рассылать уже некому
	return w.Snapshot()
}

// kick отправляет игроку последнее сообщение с причиной (Minecraft —
// экран отключения, браузер — {"t":"kick"}) отдельной пачкой, в обход
// пачки тика, и убирает его из мира. Если очередь полна, сообщение
// теряется — соединение всё равно закроется.
func (w *World) kick(p *Player, text, reason string) {
	b := GetBuffer()
	b.B = p.peer.proto.Begin(b.B)
	w.moveEv = Event{Kind: EvKick, Text: text, Player: &p.PlayerInfo}
	b.B = p.peer.proto.Append(b.B, &w.moveEv)
	w.endStats = w.stats
	w.endStats.Events = 1
	b.B = p.peer.proto.End(b.B, &w.endStats)
	if !p.peer.trySend(b) {
		PutBuffer(b)
	}
	w.remove(p, reason)
}

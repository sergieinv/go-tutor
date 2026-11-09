// Пакет metrics — метрики игрового сервера в Prometheus. Реализует
// интерфейсы world.Metrics и mc.ConnMetrics.
//
// На горячем пути (тик, запись в сокет) — только атомарные операции:
// счётчики с метками получены заранее (WithLabelValues при создании), а не
// на каждый вызов.
package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"gotutor/labs/gameserver/internal/world"
)

// Metrics — все метрики сервера.
type Metrics struct {
	tickDuration prometheus.Histogram
	tickMax      prometheus.Gauge
	ticks        prometheus.Counter
	ticksSkipped prometheus.Counter
	tps          prometheus.Gauge
	players      *prometheus.GaugeVec
	disconnects  *prometheus.CounterVec
	joinRejected *prometheus.CounterVec
	rejected     *prometheus.CounterVec
	chunksSent   prometheus.Counter
	inbox        prometheus.Gauge
	conns        *prometheus.GaugeVec
	connRejected *prometheus.CounterVec
	bytesOut     *prometheus.CounterVec
	packetsIn    *prometheus.CounterVec
	snapshots    *prometheus.CounterVec
	snapshotDur  prometheus.Histogram

	// Заранее полученные серии для двух протоколов.
	bytesMC, bytesWeb     prometheus.Counter
	packetsMC, packetsWeb prometheus.Counter
	playersMC, playersWeb prometheus.Gauge
	// Серии по причинам — заранее (их набор фиксирован): в тике — только
	// поиск в карте и атомарный инкремент, без WithLabelValues.
	disconnectBy, rejectedBy, joinRejectedBy map[string]prometheus.Counter

	// Скользящий максимум длительности тика за последние 5 секунд: по
	// гистограмме видно распределение, а пики — по этой метрике.
	mu      sync.Mutex
	maxRing [5]time.Duration
	maxSec  int64
}

// New регистрирует метрики в reg.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		tickDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "gameserver_tick_duration_seconds",
			Help: "Duration of one world tick (MSPT). Budget is 50ms at 20 TPS.",
			// 0.1 мс … 200 мс: тик здорового сервера — доли миллисекунды–единицы мс.
			Buckets: []float64{0.0001, 0.00025, 0.0005, 0.001, 0.002, 0.004, 0.006, 0.008, 0.01, 0.015, 0.02, 0.03, 0.04, 0.05, 0.075, 0.1, 0.2},
		}),
		tickMax: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gameserver_tick_duration_max_seconds",
			Help: "Maximum tick duration over the last 5 seconds.",
		}),
		ticks:        prometheus.NewCounter(prometheus.CounterOpts{Name: "gameserver_ticks_total", Help: "Ticks executed."}),
		ticksSkipped: prometheus.NewCounter(prometheus.CounterOpts{Name: "gameserver_ticks_skipped_total", Help: "Ticks dropped because the loop fell too far behind."}),
		tps:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "gameserver_tps", Help: "Measured ticks per second over the last 20 ticks (target 20)."}),
		players:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "gameserver_players", Help: "Players online by client kind."}, []string{"kind"}),
		disconnects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gameserver_disconnects_total", Help: "Players removed from the world by reason.",
		}, []string{"reason"}),
		joinRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gameserver_join_rejected_total", Help: "Join attempts rejected by the world.",
		}, []string{"reason"}),
		rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gameserver_rejected_input_total", Help: "Player input rejected by server-side validation.",
		}, []string{"reason"}),
		chunksSent: prometheus.NewCounter(prometheus.CounterOpts{Name: "gameserver_chunks_sent_total", Help: "Chunks streamed to clients."}),
		inbox:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "gameserver_inbox_length", Help: "Commands waiting in the world inbox at tick start."}),
		conns: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gameserver_connections", Help: "Open network connections.",
		}, []string{"proto"}),
		connRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gameserver_connections_rejected_total", Help: "Connections closed before joining the world.",
		}, []string{"proto", "reason"}),
		bytesOut: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gameserver_bytes_out_total", Help: "Bytes written to clients.",
		}, []string{"proto"}),
		packetsIn: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gameserver_packets_in_total", Help: "Packets (messages) received from clients.",
		}, []string{"proto"}),
		snapshots: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gameserver_snapshots_total", Help: "World snapshots by result.",
		}, []string{"result"}),
		snapshotDur: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "gameserver_snapshot_duration_seconds", Help: "Time to write a world snapshot to disk.",
			Buckets: prometheus.ExponentialBuckets(0.0005, 2, 14),
		}),
	}
	reg.MustRegister(m.tickDuration, m.tickMax, m.ticks, m.ticksSkipped, m.tps, m.players,
		m.disconnects, m.joinRejected, m.rejected, m.chunksSent, m.inbox, m.conns,
		m.connRejected, m.bytesOut, m.packetsIn, m.snapshots, m.snapshotDur)
	m.bytesMC, m.bytesWeb = m.bytesOut.WithLabelValues("minecraft"), m.bytesOut.WithLabelValues("web")
	m.packetsMC, m.packetsWeb = m.packetsIn.WithLabelValues("minecraft"), m.packetsIn.WithLabelValues("web")
	m.playersMC, m.playersWeb = m.players.WithLabelValues("minecraft"), m.players.WithLabelValues("web")
	// Серии причин — сразу с нулями: в Prometheus видно, что их ещё не было.
	m.disconnectBy = curry(m.disconnects, world.DisconnectReasons)
	m.rejectedBy = curry(m.rejected, world.RejectReasons)
	m.joinRejectedBy = curry(m.joinRejected, world.JoinRejectReasons)
	for _, r := range []string{"ok", "skipped", "error"} {
		m.snapshots.WithLabelValues(r) // ряды сразу с нулями
	}
	return m
}

// --- world.Metrics

func (m *Metrics) TickDone(d time.Duration) {
	m.tickDuration.Observe(d.Seconds())
	m.ticks.Inc()

	m.mu.Lock()
	sec := time.Now().Unix()
	if sec != m.maxSec {
		// Новая секунда: обнуляем ячейки секунд, которые прошли.
		for s := m.maxSec + 1; s <= sec && s-m.maxSec <= int64(len(m.maxRing)); s++ {
			m.maxRing[s%int64(len(m.maxRing))] = 0
		}
		if sec-m.maxSec > int64(len(m.maxRing)) {
			m.maxRing = [5]time.Duration{}
		}
		m.maxSec = sec
	}
	i := sec % int64(len(m.maxRing))
	m.maxRing[i] = max(m.maxRing[i], d)
	var mx time.Duration
	for _, v := range m.maxRing {
		mx = max(mx, v)
	}
	m.mu.Unlock()
	m.tickMax.Set(mx.Seconds())
}

func (m *Metrics) TicksSkipped(n int) { m.ticksSkipped.Add(float64(n)) }
func (m *Metrics) TPS(tps float64)    { m.tps.Set(tps) }

func (m *Metrics) Players(kind world.Kind, n int) {
	if kind == world.KindWeb {
		m.playersWeb.Set(float64(n))
	} else {
		m.playersMC.Set(float64(n))
	}
}

func (m *Metrics) Disconnect(reason string)    { inc(m.disconnectBy, m.disconnects, reason) }
func (m *Metrics) JoinRejected(reason string)  { inc(m.joinRejectedBy, m.joinRejected, reason) }
func (m *Metrics) RejectedInput(reason string) { inc(m.rejectedBy, m.rejected, reason) }

func curry(v *prometheus.CounterVec, labels []string) map[string]prometheus.Counter {
	out := make(map[string]prometheus.Counter, len(labels))
	for _, l := range labels {
		out[l] = v.WithLabelValues(l)
	}
	return out
}

// inc — заранее полученная серия; неизвестная причина (не должна
// встречаться) — через WithLabelValues.
func inc(by map[string]prometheus.Counter, v *prometheus.CounterVec, reason string) {
	if c, ok := by[reason]; ok {
		c.Inc()
		return
	}
	v.WithLabelValues(reason).Inc()
}
func (m *Metrics) ChunksSent(n int) { m.chunksSent.Add(float64(n)) }
func (m *Metrics) InboxLen(n int)   { m.inbox.Set(float64(n)) }

// --- mc.ConnMetrics (и шлюз WebSocket)

func (m *Metrics) ConnOpened(proto string) { m.conns.WithLabelValues(proto).Inc() }
func (m *Metrics) ConnClosed(proto string) { m.conns.WithLabelValues(proto).Dec() }
func (m *Metrics) ConnRejected(proto, reason string) {
	m.connRejected.WithLabelValues(proto, reason).Inc()
}

func (m *Metrics) BytesOut(proto string, n int) {
	if proto == "web" {
		m.bytesWeb.Add(float64(n))
	} else {
		m.bytesMC.Add(float64(n))
	}
}

func (m *Metrics) PacketIn(proto string) {
	if proto == "web" {
		m.packetsWeb.Inc()
	} else {
		m.packetsMC.Inc()
	}
}

// --- сохранение

// Snapshot учитывает попытку сохранения.
func (m *Metrics) Snapshot(result string, d time.Duration) {
	m.snapshots.WithLabelValues(result).Inc()
	if result == "ok" {
		m.snapshotDur.Observe(d.Seconds())
	}
}

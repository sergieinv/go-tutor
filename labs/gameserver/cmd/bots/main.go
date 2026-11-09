// Команда bots — нагрузочный прогон игрового сервера: N игроков (часть —
// по протоколу Minecraft, часть — через WebSocket) ходят и пишут в чат, а
// отчёт показывает, держит ли сервер 20 TPS: TPS, MSPT p50/p99/max по
// метрикам сервера, отключения, исходящий трафик.
//
//	bots -n 200 -duration 60s
//
// В docker compose (профиль loadtest):
//
//	docker-compose --profile labs run --rm gamebots
//	docker-compose --profile labs run --rm gamebots -n 300 -duration 2m
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ok, err := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bots:", err)
		os.Exit(1)
	}
	if !ok {
		os.Exit(2) // прогон не прошёл критерий — удобно для CI
	}
}

type botConfig struct {
	mcAddr, wsAddr, opsURL string
	n                      int
	webShare               float64
	duration, ramp         time.Duration
	spread, speed          float64
	chatEvery              time.Duration
	prefix                 string
}

// stats — счётчики ботов (атомарные: пишут сотни горутин).
type stats struct {
	joined, failed, dropped atomic.Int64
	moves, chatOut, chatIn  atomic.Int64
	packetsIn, corrections  atomic.Int64
	mu                      sync.Mutex
	dropReasons             map[string]int
}

// joinError — бот не смог войти (в отличие от отключения во время игры).
type joinError struct{ error }

func (s *stats) drop(err error) {
	var je joinError
	if !errors.As(err, &je) {
		s.dropped.Add(1)
	}
	msg := err.Error()
	if len(msg) > 60 {
		msg = msg[:60]
	}
	s.mu.Lock()
	s.dropReasons[msg]++
	s.mu.Unlock()
}

func env(getenv func(string) string, k, def string) string {
	if v := getenv(k); v != "" {
		return v
	}
	return def
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) (bool, error) {
	cfg := &botConfig{}
	fs := flag.NewFlagSet("bots", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&cfg.mcAddr, "mc", env(getenv, "GAMEBOTS_MC", "127.0.0.1:25565"), "Minecraft address")
	fs.StringVar(&cfg.wsAddr, "ws", env(getenv, "GAMEBOTS_WS", "127.0.0.1:25580"), "WebSocket gateway address")
	fs.StringVar(&cfg.opsURL, "ops", env(getenv, "GAMEBOTS_OPS", "http://127.0.0.1:25590"), "server ops URL (/metrics)")
	fs.IntVar(&cfg.n, "n", 200, "number of bots")
	fs.Float64Var(&cfg.webShare, "web", 0.25, "share of bots connected via WebSocket (0..1)")
	fs.DurationVar(&cfg.duration, "duration", 60*time.Second, "measurement duration after all bots joined")
	fs.DurationVar(&cfg.ramp, "ramp", 10*time.Second, "time to connect all bots")
	fs.Float64Var(&cfg.spread, "spread", 160, "bots walk inside [-spread, spread] blocks around spawn")
	fs.Float64Var(&cfg.speed, "speed", 4.3, "walking speed, blocks/s")
	fs.DurationVar(&cfg.chatEvery, "chat", 20*time.Second, "average interval between chat messages of one bot (0 — no chat)")
	fs.StringVar(&cfg.prefix, "prefix", "bot", "bot name prefix")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return false, err
	}
	if cfg.n < 1 || cfg.n > 9999 || cfg.webShare < 0 || cfg.webShare > 1 {
		return false, fmt.Errorf("bad -n or -web")
	}

	if err := waitReady(ctx, cfg.opsURL); err != nil {
		return false, err
	}
	st := &stats{dropReasons: map[string]int{}}
	nWeb := int(float64(cfg.n) * cfg.webShare)
	fmt.Fprintf(out, "gamebots: %d bots (%d minecraft, %d web) → mc %s, ws %s; ramp %s, measure %s\n",
		cfg.n, cfg.n-nWeb, nWeb, cfg.mcAddr, cfg.wsAddr, cfg.ramp, cfg.duration)

	botCtx, stopBots := context.WithCancel(ctx)
	defer stopBots()
	var wg sync.WaitGroup
	interval := cfg.ramp / time.Duration(cfg.n)
	for i := 0; i < cfg.n; i++ {
		name := fmt.Sprintf("%s%03d", cfg.prefix, i)
		// Браузерные боты равномерно вперемешку с Minecraft-ботами.
		web := (i*nWeb)/cfg.n != ((i+1)*nWeb)/cfg.n
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			if web {
				err = (&wsBot{name: name, cfg: cfg, stats: st}).run(botCtx)
			} else {
				err = (&mcBot{name: name, cfg: cfg, stats: st}).run(botCtx)
			}
			var je joinError
			switch {
			case botCtx.Err() != nil, err == nil:
			case errors.As(err, &je):
				st.failed.Add(1)
				st.drop(err)
			default:
				st.drop(err)
			}
		}()
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	// Дать дойти чанкам и устояться.
	time.Sleep(3 * time.Second)
	fmt.Fprintf(out, "joined %d/%d, measuring for %s…\n", st.joined.Load(), cfg.n, cfg.duration)

	before, err := scrape(cfg.opsURL)
	if err != nil {
		return false, err
	}
	var samples []sample
	tick := time.NewTicker(time.Second)
	end := time.After(cfg.duration)
	moves0, chat0, in0 := st.moves.Load(), st.chatIn.Load(), st.packetsIn.Load()
loop:
	for {
		select {
		case <-tick.C:
			m, err := scrape(cfg.opsURL)
			if err != nil {
				fmt.Fprintln(out, "scrape:", err)
				continue
			}
			samples = append(samples, sample{
				tps: m.get("gameserver_tps", ""), max: m.get("gameserver_tick_duration_max_seconds", ""),
				players: m.sum("gameserver_players"),
			})
		case <-end:
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	tick.Stop()
	after, err := scrape(cfg.opsURL)
	if err != nil {
		return false, err
	}
	moves, chat, in := st.moves.Load()-moves0, st.chatIn.Load()-chat0, st.packetsIn.Load()-in0
	stopBots()
	wg.Wait()
	return report(out, cfg, st, before, after, samples, moves, chat, in), nil
}

func waitReady(ctx context.Context, opsURL string) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		resp, err := http.Get(opsURL + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("server is not ready at %s: %v", opsURL, err)
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func splitHostPort(addr string) (string, int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 25565
	}
	n, _ := strconv.Atoi(p)
	return h, n
}

// ------------------------------------------------------ метрики сервера

type sample struct{ tps, max, players float64 }

// metrics — снятие /metrics: ряд "name{labels}" → значение.
type metrics map[string]float64

func scrape(opsURL string) (metrics, error) {
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(opsURL + "/metrics")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	m := metrics{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		i := strings.LastIndexByte(line, ' ')
		if i < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[i+1:], 64)
		if err != nil {
			continue
		}
		m[line[:i]] = v
	}
	return m, nil
}

// get — значение ряда name с метками labels (`reason="quit"`) или без.
func (m metrics) get(name, labels string) float64 {
	if labels == "" {
		return m[name]
	}
	return m[name+"{"+labels+"}"]
}

// sum — сумма всех рядов метрики.
func (m metrics) sum(name string) float64 {
	var s float64
	for k, v := range m {
		if k == name || strings.HasPrefix(k, name+"{") {
			s += v
		}
	}
	return s
}

// byLabel — ряды метрики по значению метки (для отключений по причинам).
func (m metrics) byLabel(name, label string) map[string]float64 {
	out := map[string]float64{}
	for k, v := range m {
		if !strings.HasPrefix(k, name+"{") {
			continue
		}
		key := label + `="`
		i := strings.Index(k, key)
		if i < 0 {
			continue
		}
		rest := k[i+len(key):]
		out[rest[:strings.IndexByte(rest, '"')]] = v
	}
	return out
}

// quantile — квантиль по приросту гистограммы (линейная интерполяция
// внутри корзины, как histogram_quantile в Prometheus).
func quantile(before, after metrics, name string, q float64) float64 {
	type bucket struct{ le, n float64 }
	var bs []bucket
	for k, v := range after {
		if !strings.HasPrefix(k, name+"_bucket{") {
			continue
		}
		i := strings.Index(k, `le="`)
		leStr := k[i+4 : i+4+strings.IndexByte(k[i+4:], '"')]
		le, err := strconv.ParseFloat(leStr, 64)
		if err != nil {
			continue
		}
		bs = append(bs, bucket{le, v - before[k]})
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].le < bs[j].le })
	if len(bs) == 0 || bs[len(bs)-1].n == 0 {
		return 0
	}
	total := bs[len(bs)-1].n
	rank := q * total
	prevLe, prevN := 0.0, 0.0
	for _, b := range bs {
		if b.n >= rank {
			if b.le > 1e300 { // +Inf
				return prevLe
			}
			if b.n == prevN {
				return b.le
			}
			return prevLe + (b.le-prevLe)*(rank-prevN)/(b.n-prevN)
		}
		prevLe, prevN = b.le, b.n
	}
	return prevLe
}

// ---------------------------------------------------------------- отчёт

func report(out io.Writer, cfg *botConfig, st *stats, before, after metrics, samples []sample, moves, chat, in int64) bool {
	secs := cfg.duration.Seconds()
	var tpsMin, tpsSum, maxMSPT, playersMin float64 = 1e9, 0, 0, 1e9
	for _, s := range samples {
		tpsMin = min(tpsMin, s.tps)
		tpsSum += s.tps
		maxMSPT = max(maxMSPT, s.max)
		playersMin = min(playersMin, s.players)
	}
	tpsAvg := 0.0
	if len(samples) > 0 {
		tpsAvg = tpsSum / float64(len(samples))
	} else {
		tpsMin, playersMin = 0, 0
	}
	const h = "gameserver_tick_duration_seconds"
	p50, p99 := quantile(before, after, h, 0.50)*1000, quantile(before, after, h, 0.99)*1000
	ticks := after.get("gameserver_ticks_total", "") - before.get("gameserver_ticks_total", "")
	skipped := after.get("gameserver_ticks_skipped_total", "") - before.get("gameserver_ticks_skipped_total", "")
	bytesOut := after.sum("gameserver_bytes_out_total") - before.sum("gameserver_bytes_out_total")
	rejected := after.sum("gameserver_rejected_input_total") - before.sum("gameserver_rejected_input_total")

	fmt.Fprintf(out, "\n=== gameserver load report: %d bots, %s ===\n", cfg.n, cfg.duration)
	fmt.Fprintf(out, "players online      min %.0f\n", playersMin)
	fmt.Fprintf(out, "TPS                 avg %.2f  min %.2f   (target 20)\n", tpsAvg, tpsMin)
	fmt.Fprintf(out, "ticks               %.0f (%.1f/s), skipped %.0f\n", ticks, ticks/secs, skipped)
	fmt.Fprintf(out, "MSPT                p50 %.2f ms  p99 %.2f ms  max %.2f ms   (budget 50 ms)\n", p50, p99, maxMSPT*1000)
	fmt.Fprintf(out, "traffic out         %.2f MB/s\n", bytesOut/secs/1e6)
	fmt.Fprintf(out, "bots                joined %d, failed to join %d, dropped %d, moves %.0f/s, packets in %.0f/s, chat recv %.0f/s\n",
		st.joined.Load(), st.failed.Load(), st.dropped.Load(), float64(moves)/secs, float64(in)/secs, float64(chat)/secs)
	fmt.Fprintf(out, "server corrections  %d teleports to bots, %.0f rejected inputs\n", st.corrections.Load(), rejected)
	db, da := before.byLabel("gameserver_disconnects_total", "reason"), after.byLabel("gameserver_disconnects_total", "reason")
	var reasons []string
	for r, v := range da {
		if d := v - db[r]; d > 0 {
			reasons = append(reasons, fmt.Sprintf("%s=%.0f", r, d))
		}
	}
	sort.Strings(reasons)
	if len(reasons) == 0 {
		reasons = []string{"none"}
	}
	fmt.Fprintf(out, "server disconnects  %s\n", strings.Join(reasons, " "))
	st.mu.Lock()
	for r, n := range st.dropReasons {
		fmt.Fprintf(out, "  bot error (%d): %s\n", n, r)
	}
	st.mu.Unlock()

	ok := tpsAvg >= 19.5 && tpsMin >= 18.5 && p99 < 50 && skipped == 0 && st.dropped.Load() == 0 &&
		int(st.joined.Load()) == cfg.n
	verdict := "PASS: the server holds 20 TPS"
	if !ok {
		verdict = "FAIL: see TPS / MSPT / drops above"
	}
	fmt.Fprintf(out, "verdict             %s\n", verdict)
	return ok
}

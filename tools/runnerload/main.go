// Команда runnerload — нагрузочный тест локального runner через nginx.
//
// Клиенты гоняют те же запросы, что делает сайт (/api/run примеров,
// /api/test эталонов, /api/gotest, /api/trace), и сверяют результат с
// example/output.txt. Сценарий abuse добавляет к ним «злые» запросы:
// бесконечный цикл, fork-бомба, выделение памяти, бесконечный вывод.
//
//	# обычная нагрузка, 8 клиентов, 30 секунд
//	go run ./tools/runnerload -clients 8 -duration 30s
//
//	# нагрузка со злоупотреблением (20 % запросов — «злые»)
//	go run ./tools/runnerload -clients 8 -duration 60s -scenario abuse
//
// Числа машинозависимы: это не бенчмарк runner, а проверка, что под
// нагрузкой он остаётся корректным и не падает.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "runnerload:", err)
		os.Exit(1)
	}
}

type options struct {
	target     string
	content    string
	clients    int
	duration   time.Duration
	scenario   string
	abuseShare float64
	timeout    time.Duration
	verify     bool
	fillTmpfs  bool
	seed       uint64
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	var o options
	fs := flag.NewFlagSet("runnerload", flag.ContinueOnError)
	fs.StringVar(&o.target, "url", "http://127.0.0.1:8080", "адрес сайта (nginx), через который идут запросы")
	fs.StringVar(&o.content, "content", "content", "каталог контента уроков")
	fs.IntVar(&o.clients, "clients", 8, "число параллельных клиентов")
	fs.DurationVar(&o.duration, "duration", 30*time.Second, "длительность прогона")
	fs.StringVar(&o.scenario, "scenario", "normal", "сценарий: normal | abuse")
	fs.Float64Var(&o.abuseShare, "abuse-share", 0.2, "доля «злых» запросов в сценарии abuse")
	fs.DurationVar(&o.timeout, "timeout", 180*time.Second, "таймаут одного HTTP-запроса")
	fs.BoolVar(&o.verify, "verify", true, "сверять stdout примеров с example/output.txt")
	fs.BoolVar(&o.fillTmpfs, "fill-tmpfs", false, "добавить в abuse запрос, забивающий tmpfs рабочих каталогов")
	fs.Uint64Var(&o.seed, "seed", 1, "зерно выбора запросов")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.clients < 1 {
		return errors.New("-clients: нужно положительное число")
	}
	if o.scenario != "normal" && o.scenario != "abuse" {
		return fmt.Errorf("-scenario: неизвестный сценарий %q", o.scenario)
	}

	lessons, err := loadLessons(o.content)
	if err != nil {
		return err
	}
	normal := normalTasks(lessons, o.verify)
	var abuse []task
	if o.scenario == "abuse" {
		for _, t := range abuseTasks() {
			if t.label == "abuse:tmpfs" && !o.fillTmpfs {
				continue
			}
			abuse = append(abuse, t)
		}
	}

	base, err := url.Parse(o.target)
	if err != nil {
		return fmt.Errorf("-url: %w", err)
	}
	client := &http.Client{
		Timeout: o.timeout,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: o.clients,
			DisableCompression:  true,
		},
	}
	if err := health(ctx, client, base); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "runnerload: %s, сценарий %s, клиентов %d, %s\n", base, o.scenario, o.clients, o.duration)
	fmt.Fprintf(stdout, "уроков в наборе: %d, обычных запросов: %d, «злых»: %d\n\n", len(lessons), len(normal), len(abuse))

	st := &stats{byLabel: map[string]*labelStats{}, statuses: map[int]int{}, errors: map[string]int{}}
	deadline := time.Now().Add(o.duration)
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	var wg sync.WaitGroup
	start := time.Now()
	for c := range o.clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rnd := rand.New(rand.NewPCG(o.seed, uint64(c)))
			for runCtx.Err() == nil {
				t := pick(rnd, normal, abuse, o.abuseShare)
				st.add(do(runCtx, client, base, t))
			}
		}()
	}
	wg.Wait()
	st.wall = time.Since(start)
	report(stdout, st, o)
	return nil
}

// pick выбирает следующий запрос: с вероятностью share — «злой».
func pick(rnd *rand.Rand, normal, abuse []task, share float64) task {
	if len(abuse) > 0 && rnd.Float64() < share {
		return abuse[rnd.IntN(len(abuse))]
	}
	return normal[rnd.IntN(len(normal))]
}

// health ждёт, пока runner ответит на /api/health.
func health(ctx context.Context, client *http.Client, base *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var last error
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base.JoinPath("/api/health").String(), nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		resp, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		last = err
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("runner не отвечает на /api/health: %w", last)
}

// outcome — результат одного запроса.
type outcome struct {
	label  string
	lesson string
	status int
	// latency — полное время запроса, durationMs — сколько из него занял
	// запуск в песочнице. Разница — ожидание слота, сборка и сеть.
	latency time.Duration
	sandbox time.Duration
	err     error
}

// do выполняет один запрос с заголовками, которые ставит браузер на
// странице учебника: без них runner отвергает запрос как межсайтовый.
func do(ctx context.Context, client *http.Client, base *url.URL, t task) outcome {
	u := base.JoinPath(t.path).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(t.body))
	if err != nil {
		return outcome{label: t.label, lesson: t.lesson, err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base.String())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return outcome{label: t.label, lesson: t.lesson, err: nil, status: -1} // прогон кончился
		}
		return outcome{label: t.label, lesson: t.lesson, latency: time.Since(start), err: fmt.Errorf("транспорт: %v", err)}
	}
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	latency := time.Since(start)

	var r apiResp
	if rerr == nil {
		// Ответ не-JSON (например, страница ошибки nginx) — это находка.
		if err := json.Unmarshal(body, &r); err != nil {
			rerr = fmt.Errorf("ответ не JSON: %.80s", body)
		}
	}
	out := outcome{label: t.label, lesson: t.lesson, status: resp.StatusCode, latency: latency,
		sandbox: time.Duration(r.DurationMs) * time.Millisecond}
	switch {
	case rerr != nil:
		out.err = rerr
	default:
		out.err = t.check(resp.StatusCode, &r)
	}
	return out
}

type labelStats struct {
	total     int
	failed    int
	latencies []time.Duration
	waits     []time.Duration
}

type stats struct {
	mu       sync.Mutex
	wall     time.Duration
	total    int
	failed   int
	byLabel  map[string]*labelStats
	statuses map[int]int
	errors   map[string]int
}

func (s *stats) add(o outcome) {
	if o.status == -1 && o.err == nil {
		return // запрос оборван концом прогона
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	s.statuses[o.status]++
	ls := s.byLabel[o.label]
	if ls == nil {
		ls = &labelStats{}
		s.byLabel[o.label] = ls
	}
	ls.total++
	ls.latencies = append(ls.latencies, o.latency)
	if wait := o.latency - o.sandbox; wait > 0 {
		ls.waits = append(ls.waits, wait)
	}
	if o.err != nil {
		s.failed++
		ls.failed++
		where := o.label
		if o.lesson != "" {
			where += " " + o.lesson
		}
		s.errors[where+": "+o.err.Error()]++
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p * float64(len(sorted)-1) / 100)
	return sorted[i]
}

func report(w io.Writer, s *stats, o options) {
	fmt.Fprintf(w, "запросов: %d за %s — %.1f запр/с, ошибок: %d\n",
		s.total, s.wall.Round(time.Millisecond), float64(s.total)/s.wall.Seconds(), s.failed)

	codes := slices.Sorted(maps(s.statuses))
	var parts []string
	for _, c := range codes {
		parts = append(parts, fmt.Sprintf("%d: %d", c, s.statuses[c]))
	}
	fmt.Fprintf(w, "коды ответов: %s\n\n", strings.Join(parts, ", "))

	fmt.Fprintf(w, "%-18s %7s %7s %9s %9s %9s %9s\n", "запрос", "всего", "ошибок", "p50", "p90", "p99", "ожид.p90")
	for _, label := range slices.Sorted(maps(s.byLabel)) {
		ls := s.byLabel[label]
		sort.Slice(ls.latencies, func(i, j int) bool { return ls.latencies[i] < ls.latencies[j] })
		sort.Slice(ls.waits, func(i, j int) bool { return ls.waits[i] < ls.waits[j] })
		fmt.Fprintf(w, "%-18s %7d %7d %9s %9s %9s %9s\n", label, ls.total, ls.failed,
			ms(percentile(ls.latencies, 50)), ms(percentile(ls.latencies, 90)),
			ms(percentile(ls.latencies, 99)), ms(percentile(ls.waits, 90)))
	}

	if len(s.errors) > 0 {
		fmt.Fprintln(w, "\nошибки по видам:")
		for _, k := range slices.Sorted(maps(s.errors)) {
			fmt.Fprintf(w, "  %4d × %s\n", s.errors[k], k)
		}
	}

	fmt.Fprintln(w, "\nкритерии:")
	server := 0
	for code, n := range s.statuses {
		if code >= 500 && code != 503 {
			server += n
		}
	}
	check(w, "нет 5xx, кроме 503 «слоты заняты»", server == 0, fmt.Sprintf("%d ответов", server))
	normalFailed := 0
	for label, ls := range s.byLabel {
		if !strings.HasPrefix(label, "abuse:") {
			normalFailed += ls.failed
		}
	}
	check(w, "обычные запросы корректны (вывод сверен с output.txt)", normalFailed == 0, fmt.Sprintf("%d ошибок", normalFailed))
	abuseFailed := 0
	for label, ls := range s.byLabel {
		if strings.HasPrefix(label, "abuse:") {
			abuseFailed += ls.failed
		}
	}
	if o.scenario == "abuse" {
		check(w, "«злые» запросы удержаны лимитами", abuseFailed == 0, fmt.Sprintf("%d ошибок", abuseFailed))
	}
	busy := s.statuses[503]
	fmt.Fprintf(w, "  i  503 «все песочницы заняты»: %d (%.1f %%) — ожидание слота дольше 30 с\n",
		busy, 100*float64(busy)/float64(max(s.total, 1)))
}

func check(w io.Writer, name string, ok bool, detail string) {
	mark := "FAIL"
	if ok {
		mark = "ok"
	}
	fmt.Fprintf(w, "  %-4s %s (%s)\n", mark, name, detail)
}

func ms(d time.Duration) string { return fmt.Sprintf("%d мс", d.Milliseconds()) }

// maps — ключи отображения (slices.Sorted принимает итератор).
func maps[K cmp, V any](m map[K]V) func(func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

type cmp interface{ ~string | ~int }

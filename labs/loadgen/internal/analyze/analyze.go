// Package analyze — нагрузочный разбор итогового проекта одной командой.
//
// Разбор прогоняет одну и ту же смесь запросов (открытая модель) по шагам
// и между шагами переключает функции «Заказов» через служебный эндпоинт
// /admin/features — без пересборки и перезапуска:
//
//  1. базовый прогон: кеш каталога и чтение с реплики выключены — видно,
//     сколько запросов каталога доходит до базы и какие задержки;
//  2. + кеш каталога (Valkey): запросы каталога к базе почти исчезают;
//  3. + чтение истории с реплики: история уходит на реплику, а проверка
//     read-your-writes показывает, что без токена свежий заказ не виден,
//     а с токеном — виден;
//  4. перегрузка CreateOrder: заказы с частотой выше, чем сервис
//     успевает, — лишние получают 429 быстро, а не таймаут.
//
// До и после каждого шага снимаются метрики Prometheus с /metrics
// «Заказов», в отчёт идёт разница. В конце — сводная таблица «до/после».
// Абсолютные числа зависят от машины; смотреть стоит на направление.
package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gotutor/labs/loadgen/internal/load"
	"gotutor/labs/loadgen/internal/promtext"
)

// Config — параметры разбора.
type Config struct {
	Target string // REST «Заказов», http://orders:8080
	Ops    string // служебный порт, http://orders:8081
	Rate   float64
	// Duration — длительность каждого шага; Warmup — прогрев перед шагом
	// (его статистика не учитывается).
	Duration, Warmup time.Duration
	Mix              string
	Options          load.Options
	// OverloadRate, OverloadDuration — шаг 4: только заказы; 0 — без шага.
	OverloadRate     float64
	OverloadDuration time.Duration
	Timeout          time.Duration
	Seed             uint64
	Out              io.Writer
	// ReadyTimeout — сколько ждать готовности «Заказов».
	ReadyTimeout time.Duration
}

type features struct {
	CatalogCache *bool `json:"catalog_cache,omitempty"`
	ReplicaReads *bool `json:"replica_reads,omitempty"`
}

func ptr(b bool) *bool { return &b }

type step struct {
	name           string
	cache, replica bool
	overload       bool
	seed           uint64
}

// result — итог шага для сводной таблицы.
type result struct {
	step    step
	skipped string
	stats   *load.Stats
	delta   delta
}

// delta — разница метрик сервиса за шаг.
type delta struct {
	secs                                float64
	dbQueries                           float64
	hits, misses, negHits, cacheErrors  float64
	replicaReads, primaryReads, lagging float64
	replicaErrors                       float64
	shed                                float64
}

// Run выполняет разбор.
func Run(ctx context.Context, cfg Config) error {
	out := cfg.Out
	hc := &http.Client{Timeout: 5 * time.Second}

	fmt.Fprintf(out, "Нагрузочный разбор «Заказов»: %s (служебный порт %s)\n", cfg.Target, cfg.Ops)
	if err := waitReady(ctx, hc, cfg.Target+"/readyz", cfg.ReadyTimeout); err != nil {
		return err
	}
	initial, err := getFeatures(ctx, hc, cfg.Ops)
	if err != nil {
		return err
	}
	// Вернуть переключатели как были — даже при ошибке или Ctrl+C.
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := setFeatures(rctx, hc, cfg.Ops, initial); err != nil {
			fmt.Fprintf(out, "не удалось вернуть переключатели %+v: %v\n", initial, err)
		}
	}()
	fmt.Fprintf(out, "переключатели сейчас: кеш каталога %s, чтение истории с реплики %s\n",
		onOff(initial.CatalogCache), onOff(initial.ReplicaReads))
	fmt.Fprintf(out, "смесь %q, %.0f сценариев/с, шаг %s (+ прогрев %s)\n\n", cfg.Mix, cfg.Rate, cfg.Duration, cfg.Warmup)

	steps := []step{
		{name: "1 база", cache: false, replica: false},
		{name: "2 +кеш", cache: true, replica: false},
		{name: "3 +реплика", cache: true, replica: true},
	}
	if cfg.OverloadRate > 0 && cfg.OverloadDuration > 0 {
		// Переключатели не трогаем: остаются как после последнего шага.
		steps = append(steps, step{name: "4 перегрузка", overload: true})
	}

	var results []result
	for i, st := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Своё зерно на шаг: иначе следующий шаг повторит те же
		// несуществующие id каталога, и их отдаст отрицательный кеш
		// предыдущего шага — «БД кат.» станет нулём не благодаря кешу.
		st.seed = cfg.Seed + uint64(i)*1_000_003
		r, err := runStep(ctx, hc, cfg, st)
		if err != nil {
			return err
		}
		results = append(results, r)
	}
	summary(out, results)
	return nil
}

func runStep(ctx context.Context, hc *http.Client, cfg Config, st step) (result, error) {
	out := cfg.Out
	res := result{step: st}
	if st.overload {
		fmt.Fprintf(out, "── шаг %s: переключатели как в предыдущем шаге ──\n", st.name)
	} else {
		fmt.Fprintf(out, "── шаг %s: кеш каталога %s, реплика %s ──\n", st.name, onOffB(st.cache), onOffB(st.replica))
		code, err := setFeatures(ctx, hc, cfg.Ops, features{CatalogCache: ptr(st.cache), ReplicaReads: ptr(st.replica)})
		if code == http.StatusConflict {
			res.skipped = "функция не настроена в «Заказах»"
			fmt.Fprintf(out, "пропущен: %s (ORDERS_VALKEY_ADDR / ORDERS_REPLICA_DSN)\n\n", res.skipped)
			return res, nil
		}
		if err != nil {
			return res, err
		}
	}

	mixSpec, rate, dur := cfg.Mix, cfg.Rate, cfg.Duration
	if st.overload {
		mixSpec, rate, dur = "orders", cfg.OverloadRate, cfg.OverloadDuration
		fmt.Fprintf(out, "только заказы, %.0f/с — больше, чем пропускает ограничение CreateOrder\n", rate)
	}
	opts := cfg.Options
	if st.overload {
		opts.RYW = false
	}

	if cfg.Warmup > 0 && !st.overload {
		warm := load.NewStats()
		mix, err := load.ParseMix(mixSpec, load.Scenarios(opts, warm))
		if err != nil {
			return res, err
		}
		load.RunOpen(ctx, load.NewClient(cfg.Target, cfg.Timeout, warm), mix,
			load.OpenConfig{Rate: rate, Duration: cfg.Warmup, Seed: st.seed + 1000}, warm)
	}

	before, err := scrape(ctx, hc, cfg.Ops)
	if err != nil {
		return res, err
	}
	stats := load.NewStats()
	mix, err := load.ParseMix(mixSpec, load.Scenarios(opts, stats))
	if err != nil {
		return res, err
	}
	load.RunOpen(ctx, load.NewClient(cfg.Target, cfg.Timeout, stats), mix,
		load.OpenConfig{Rate: rate, Duration: dur, Seed: st.seed}, stats)
	after, err := scrape(ctx, hc, cfg.Ops)
	if err != nil {
		return res, err
	}
	res.stats = stats
	res.delta = diff(before, after, stats.Elapsed.Seconds())

	stats.Report(out)
	d := res.delta
	fmt.Fprintf(out, "метрики «Заказов» за шаг:\n")
	fmt.Fprintf(out, "  запросов каталога в базу: %.0f (%.1f/с)", d.dbQueries, d.dbQueries/d.secs)
	if lookups := d.hits + d.misses + d.negHits; lookups > 0 {
		fmt.Fprintf(out, "; кеш: попаданий %.0f, «нет такого» из кеша %.0f, промахов %.0f, ошибок Valkey %.0f",
			d.hits, d.negHits, d.misses, d.cacheErrors)
	}
	fmt.Fprintln(out)
	if reads := d.replicaReads + d.primaryReads; reads > 0 {
		fmt.Fprintf(out, "  история: с реплики %.0f, с основной %.0f (из них реплика отставала от токена: %.0f)\n",
			d.replicaReads, d.primaryReads, d.lagging)
		if d.replicaErrors > 0 {
			fmt.Fprintf(out, "  реплика недоступна: %.0f чтений ушли на основную (деградация, ошибок клиенту нет) — проверьте postgres-replica\n",
				d.replicaErrors)
		}
	}
	fmt.Fprintf(out, "  отброшено ограничением CreateOrder (429): %.0f\n\n", d.shed)
	return res, nil
}

func diff(a, b promtext.Snapshot, secs float64) delta {
	sub := func(name string, match map[string]string) float64 {
		return b.Increase(a, name, match)
	}
	return delta{
		secs:          max(secs, 0.001),
		dbQueries:     sub("orders_catalog_db_queries_total", nil),
		hits:          sub("orders_catalog_cache_lookups_total", map[string]string{"result": "hit"}),
		misses:        sub("orders_catalog_cache_lookups_total", map[string]string{"result": "miss"}),
		negHits:       sub("orders_catalog_cache_lookups_total", map[string]string{"result": "negative_hit"}),
		cacheErrors:   sub("orders_catalog_cache_lookups_total", map[string]string{"result": "error"}),
		replicaReads:  sub("orders_history_reads_total", map[string]string{"target": "replica"}),
		primaryReads:  sub("orders_history_reads_total", map[string]string{"target": "primary"}),
		lagging:       sub("orders_history_reads_total", map[string]string{"reason": "lagging"}),
		replicaErrors: sub("orders_history_reads_total", map[string]string{"reason": "replica_error"}),
		shed:          sub("orders_shed_rejected_total", nil),
	}
}

// summary — сводная таблица «до/после».
func summary(out io.Writer, results []result) {
	fmt.Fprintln(out, "══ сводка ══")
	fmt.Fprintf(out, "%-13s %7s %9s %9s %10s %10s %7s %6s %6s %9s %8s %8s %11s\n",
		"шаг", "сцен/с", "каталог", "каталог", "заказ", "история", "5xx/", "429", "400", "БД кат.", "кеш", "реплика", "не видно")
	fmt.Fprintf(out, "%-13s %7s %9s %9s %10s %10s %7s %6s %6s %9s %8s %8s %11s\n",
		"", "", "p50", "p99", "p99", "p99", "сеть", "", "", "в сек", "hit", "доля", "без/с ток.")
	for _, r := range results {
		if r.skipped != "" {
			fmt.Fprintf(out, "%-13s пропущен: %s\n", r.step.name, r.skipped)
			continue
		}
		s, d := r.stats, r.delta
		cat, create, list := s.Op("catalog.get"), s.Op("orders.create"), s.Op("orders.list")
		// 404 каталога — нарочно (несуществующие товары), не ошибка.
		var errs, rejected, bad int64
		for _, name := range s.Ops() {
			for outcome, n := range s.Op(name).Outcomes {
				switch {
				case outcome == "429":
					rejected += n
				case outcome == "400":
					bad += n
				case outcome == "canceled", outcome[0] == '2', outcome[0] == '4':
				default: // 5xx, timeout, conn_error
					errs += n
				}
			}
		}
		hit := "—"
		if lookups := d.hits + d.misses + d.negHits; lookups > 0 {
			hit = fmt.Sprintf("%.0f%%", 100*(d.hits+d.negHits)/lookups)
		}
		replica := "—"
		if reads := d.replicaReads + d.primaryReads; reads > 0 {
			replica = fmt.Sprintf("%.0f%%", 100*d.replicaReads/reads)
		}
		stale := "—"
		if s.RYWChecks > 0 {
			stale = fmt.Sprintf("%d/%d", s.StaleNoToken, s.StaleWithToken)
		}
		fmt.Fprintf(out, "%-13s %7.0f %9s %9s %10s %10s %7d %6d %6d %9.1f %8s %8s %11s\n",
			r.step.name, float64(s.Started)/max(s.Elapsed.Seconds(), 0.001),
			q(cat, 0.5), q(cat, 0.99), q(create, 0.99), q(list, 0.99),
			errs, rejected, bad, d.dbQueries/d.secs, hit, replica, stale)
		if r.step.overload && create != nil && create.Outcomes["429"] > 0 {
			fmt.Fprintf(out, "%-13s заказ: p99 принятых (200) %s, p99 отказов (429) %s — отказ не ждёт дедлайна\n",
				"", load.Ms(create.OutcomeQuantile("200", 0.99)), load.Ms(create.OutcomeQuantile("429", 0.99)))
		} else if r.step.overload {
			fmt.Fprintf(out, "%-13s отказов 429 нет: сервис успел — поднимите -overload-rate\n", "")
		}
	}
	fmt.Fprintln(out, `
Как читать: «БД кат. в сек» — запросы каталога, дошедшие до PostgreSQL; с
кешем их почти нет: остаются промахи по id, которых нет в каталоге
(отрицательный кеш спасает только от повторов), и раз в TTL. «реплика
доля» — сколько чтений истории ушло на реплику. «не видно без/с ток.» —
проверки read-your-writes сразу после создания заказа: без токена с
реплики заказ часто не виден (задержка 200 мс), с токеном — никогда. «429» — отказы
ограничения CreateOrder: в шаге перегрузки они быстрые, а задержка
принятых заказов остаётся ограниченной. «400» — отказы склада (не хватило
товара), «5xx/сеть» — настоящие ошибки. Числа зависят от машины —
сравнивайте шаги между собой, а не с чужими цифрами.`)
}

func q(o *load.OpStats, p float64) string {
	if o.Count() == 0 {
		return "—"
	}
	return load.Ms(o.Quantile(p))
}

func onOff(b *bool) string {
	if b == nil {
		return "?"
	}
	return onOffB(*b)
}

func onOffB(b bool) string {
	if b {
		return "вкл"
	}
	return "выкл"
}

// waitReady ждёт /readyz «Заказов»: при старте стенда разбор могут
// запустить раньше, чем сервис поднялся.
func waitReady(ctx context.Context, hc *http.Client, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := hc.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("«Заказы» не готовы (%s): %v — запущен ли профиль labs?", url, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

var errNoAdmin = errors.New("эндпоинт /admin/features выключен: нужен ORDERS_ADMIN_FEATURES=true в окружении «Заказов» (в docker-compose.yml он включён)")

func getFeatures(ctx context.Context, hc *http.Client, ops string) (features, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ops+"/admin/features", nil)
	resp, err := hc.Do(req)
	if err != nil {
		return features{}, fmt.Errorf("служебный порт «Заказов»: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return features{}, errNoAdmin
	}
	var f features
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<10)).Decode(&f); err != nil || resp.StatusCode != http.StatusOK {
		return features{}, fmt.Errorf("GET /admin/features: %d %v", resp.StatusCode, err)
	}
	return f, nil
}

func setFeatures(ctx context.Context, hc *http.Client, ops string, f features) (int, error) {
	b, _ := json.Marshal(f)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ops+"/admin/features", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.StatusCode, nil
	case http.StatusNotFound:
		return resp.StatusCode, errNoAdmin
	}
	return resp.StatusCode, fmt.Errorf("POST /admin/features: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func scrape(ctx context.Context, hc *http.Client, ops string) (promtext.Snapshot, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ops+"/metrics", nil)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("метрики «Заказов»: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /metrics: %d", resp.StatusCode)
	}
	return promtext.Parse(io.LimitReader(resp.Body, 8<<20))
}

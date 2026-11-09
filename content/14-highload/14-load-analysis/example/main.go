// Нагрузочный разбор `labs/` в миниатюре: «Заказы» на `httptest` с
// «базой»-счётчиком, кешем каталога, отстающей репликой и ограничителем
// CreateOrder. Разбор, как `loadgen analyze`, переключает функции через
// служебный эндпоинт, снимает `/metrics` до и после шага и печатает
// разницу. Вместо частоты в секунду — одна и та же последовательность
// сценариев, а реплика «догоняет» по команде: вывод не зависит от машины.
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Метрики — с теми же именами и метками, что у «Заказов» в labs.
var (
	reg       = prometheus.NewRegistry()
	dbQueries = prometheus.NewCounter(prometheus.CounterOpts{Name: "orders_catalog_db_queries_total", Help: "Catalog queries that reached the db."})
	lookups   = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "orders_catalog_cache_lookups_total", Help: "Cache lookups."}, []string{"result"})
	history   = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "orders_history_reads_total", Help: "History reads."}, []string{"target", "reason"})
	shed      = prometheus.NewCounter(prometheus.CounterOpts{Name: "orders_shed_rejected_total", Help: "CreateOrder rejected with 429."})
)

// Сервис: каталог в «базе», заказы на основной, реплика видит первые
// `applied` записей. `slots` — ограничитель CreateOrder без очереди.
type service struct {
	mu        sync.Mutex
	cacheOn   bool
	replicaOn bool
	cache     map[int]bool // id → есть ли товар; false — «нет такого»
	customers []int        // заказы на основной: покупатель каждой записи
	applied   int          // сколько записей проиграла реплика
	slots     chan struct{}
	hold      chan struct{} // не nil — CreateOrder «застревает» (перегрузка)
}

// Каталог: cache-aside и отрицательный кеш. Выключенный кеш — `bypass`,
// каждый запрос идёт в «базу» (в ней товары 1–4).
func (s *service) product(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cacheOn {
		lookups.WithLabelValues("bypass").Inc()
		dbQueries.Inc()
		return id >= 1 && id <= 4
	}
	if found, ok := s.cache[id]; ok {
		lookups.WithLabelValues(map[bool]string{true: "hit", false: "negative_hit"}[found]).Inc()
		return found
	}
	lookups.WithLabelValues("miss").Inc()
	dbQueries.Inc()
	s.cache[id] = id >= 1 && id <= 4
	return s.cache[id]
}

// CreateOrder: занять место или сразу 429 с `Retry-After`. Цена — из
// каталога. Токен — позиция записи на основной (в labs — LSN).
func (s *service) create(w http.ResponseWriter, r *http.Request) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		shed.Inc()
		w.Header().Set("Retry-After", "1")
		http.Error(w, "overloaded", http.StatusTooManyRequests)
		return
	}
	s.mu.Lock()
	hold := s.hold
	s.mu.Unlock()
	if hold != nil {
		<-hold
	}
	s.product(4)
	customer, _ := strconv.Atoi(r.URL.Query().Get("customer"))
	s.mu.Lock()
	s.customers = append(s.customers, customer)
	token := len(s.customers)
	s.mu.Unlock()
	fmt.Fprintf(w, "%d", token)
}

// ListOrders: с реплики, если она включена и проиграла позицию токена;
// без токена — с реплики всегда. Ответ — сколько заказов покупателя видно.
func (s *service) list(w http.ResponseWriter, r *http.Request) {
	customer, _ := strconv.Atoi(r.URL.Query().Get("customer"))
	token, _ := strconv.Atoi(r.URL.Query().Get("token"))
	s.mu.Lock()
	defer s.mu.Unlock()
	visible, target, reason := len(s.customers), "primary", "disabled"
	switch {
	case s.replicaOn && token <= s.applied:
		visible, target, reason = s.applied, "replica", "no_token"
		if token > 0 {
			reason = "caught_up"
		}
	case s.replicaOn:
		reason = "lagging"
	}
	history.WithLabelValues(target, reason).Inc()
	n := 0
	for _, c := range s.customers[:visible] {
		if c == customer {
			n++
		}
	}
	fmt.Fprintf(w, "%d", n)
}

// replicate — «прошло 200 мс»: реплика проиграла всё, что было записано.
func (s *service) replicate() {
	s.mu.Lock()
	s.applied = len(s.customers)
	s.mu.Unlock()
}

func main() {
	reg.MustRegister(dbQueries, lookups, history, shed)
	svc := &service{cache: map[int]bool{}, slots: make(chan struct{}, 2)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		if !svc.product(id) {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("POST /v1/orders", svc.create)
	mux.HandleFunc("GET /v1/orders", svc.list)
	// В labs переключатели и /metrics — на отдельном служебном порту,
	// опубликованном только на 127.0.0.1; здесь всё на одном loopback.
	mux.HandleFunc("POST /admin/features", func(w http.ResponseWriter, r *http.Request) {
		svc.mu.Lock()
		svc.cacheOn, svc.replicaOn = r.URL.Query().Get("cache") == "on", r.URL.Query().Get("replica") == "on"
		svc.mu.Unlock()
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := httptest.NewUnstartedServer(mux)
	srv.Config.ReadHeaderTimeout = time.Second
	srv.Start()
	defer srv.Close()
	c := &client{base: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	// Шаги разбора: те же, что у `loadgen analyze`.
	fmt.Printf("%-13s %9s %8s %8s %8s %12s %5s\n", "шаг", "сценариев", "БД кат.", "кеш hit", "реплика", "не видно", "429")
	fmt.Printf("%-13s %9s %8s %8s %8s %12s\n", "", "", "", "", "", "без/с ток.")
	for step, sw := range []struct{ name, cache, replica string }{
		{"1 база", "off", "off"}, {"2 +кеш", "on", "off"}, {"3 +реплика", "on", "on"},
	} {
		c.do("POST", "/admin/features?cache="+sw.cache+"&replica="+sw.replica)
		// Прогрев: по запросу на товар. Его метрики в шаг не входят.
		for id := 1; id <= 4; id++ {
			c.do("GET", "/v1/products/"+strconv.Itoa(id))
		}
		before := c.scrape()
		var staleNo, staleTok int
		for i := range 20 {
			switch k := i % 10; {
			case k < 5: // каталог: существующие товары
				c.do("GET", "/v1/products/"+strconv.Itoa(1+i%3))
			case k == 5: // каталог: несуществующий id, новый на каждом шаге
				c.do("GET", "/v1/products/"+strconv.Itoa(1000*(step+1)+i))
			case k == 6: // заказ и проверка read-your-writes
				customer := strconv.Itoa(1000*(step+1) + i) // новый покупатель
				_, token := c.do("POST", "/v1/orders?customer="+customer)
				if _, n := c.do("GET", "/v1/orders?customer="+customer); n == "0" {
					staleNo++
				}
				if _, n := c.do("GET", "/v1/orders?customer="+customer+"&token="+token); n == "0" {
					staleTok++
				}
				svc.replicate()
			default: // история других покупателей
				c.do("GET", "/v1/orders?customer="+strconv.Itoa(i))
			}
		}
		row(sw.name, 20, before, c.scrape(), fmt.Sprintf("%d/%d", staleNo, staleTok))
	}

	// Шаг 4, перегрузка: шесть заказов разом, а CreateOrder «застрял».
	// Двое заняли оба места, остальные получили 429 сразу, не дожидаясь.
	before := c.scrape()
	svc.mu.Lock()
	svc.hold = make(chan struct{})
	svc.mu.Unlock()
	codes := make([]int, 6)
	var wg sync.WaitGroup
	var answered atomic.Int64
	for i := range codes {
		wg.Go(func() {
			codes[i], _ = c.do("POST", "/v1/orders?customer=7")
			answered.Add(1)
		})
	}
	for len(svc.slots) < 2 || answered.Load() < 4 {
		time.Sleep(time.Millisecond)
	}
	close(svc.hold) // база «отпустила»: принятые доработали
	wg.Wait()
	row("4 перегрузка", 6, before, c.scrape(), "—")
	ok, rejected := 0, 0
	for _, code := range codes {
		if code == http.StatusOK {
			ok++
		} else if code == http.StatusTooManyRequests {
			rejected++
		}
	}
	fmt.Printf("перегрузка: принято %d, отказ 429 — %d (Retry-After: 1)\n", ok, rejected)
}

// row печатает строку сводки: разница счётчиков между снимками.
func row(name string, scenarios int, before, after map[string]*dto.MetricFamily, stale string) {
	d := func(metric string, kv ...string) float64 {
		return sum(after, metric, kv...) - sum(before, metric, kv...)
	}
	hits := d("orders_catalog_cache_lookups_total", "result", "hit") + d("orders_catalog_cache_lookups_total", "result", "negative_hit")
	looked := hits + d("orders_catalog_cache_lookups_total", "result", "miss")
	replica := d("orders_history_reads_total", "target", "replica")
	reads := replica + d("orders_history_reads_total", "target", "primary")
	fmt.Printf("%-13s %9d %8.0f %8s %8s %12s %5.0f\n", name, scenarios, d("orders_catalog_db_queries_total"),
		pct(hits, looked), pct(replica, reads), stale, d("orders_shed_rejected_total"))
}

// client — минимальный клиент разбора: код ответа и тело.
type client struct {
	base string
	http *http.Client
}

func (c *client) do(method, path string) (int, string) {
	req, _ := http.NewRequest(method, c.base+path, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, strings.TrimSpace(string(body))
}

// scrape снимает /metrics и разбирает текстовый формат готовым парсером
// из `prometheus/common/expfmt` — так делают в продакшне. В задании урока
// парсер пишется руками.
func (c *client) scrape() map[string]*dto.MetricFamily {
	_, text := c.do("GET", "/metrics")
	p := expfmt.NewTextParser(model.UTF8Validation)
	fams, err := p.TextToMetricFamilies(strings.NewReader(text + "\n"))
	if err != nil {
		panic(err)
	}
	return fams
}

// sum — сумма рядов счётчика name, у которых есть все метки kv.
func sum(fams map[string]*dto.MetricFamily, name string, kv ...string) float64 {
	var total float64
next:
	for _, m := range fams[name].GetMetric() {
		labels := map[string]string{}
		for _, l := range m.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		for i := 0; i+1 < len(kv); i += 2 {
			if labels[kv[i]] != kv[i+1] {
				continue next
			}
		}
		total += m.GetCounter().GetValue()
	}
	return total
}

func pct(a, b float64) string {
	if b == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", 100*a/b)
}

// Метрики — числа, которые сервис копит в памяти, а
// Prometheus раз в 15–30 секунд забирает с `/metrics`. Пример:
// middleware считает RED — Rate (запросы), Errors (коды
// ответа) и Duration (гистограмма длительности) — для
// HTTP API «Заказов», а потом сам «скрейпит» свой `/metrics`.

package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Метрики собраны в структуру и регистрируются в своём
// реестре — не в глобальном `prometheus.DefaultRegisterer`:
// так их можно создать заново в каждом тесте.
type httpMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func newHTTPMetrics(reg prometheus.Registerer) *httpMetrics {
	m := &httpMetrics{
		// Counter только растёт; Prometheus считает из него
		// скорость: `rate(http_requests_total[5m])`.
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP-запросы по маршруту и коду ответа.",
		}, []string{"route", "code"}),
		// Histogram раскладывает наблюдения по бакетам
		// «не больше le»; из них считают перцентили.
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Длительность обработки HTTP-запроса.",
			Buckets: []float64{0.025, 0.1, 0.5},
		}, []string{"route"}),
		// Gauge ходит вверх и вниз: сколько запросов сейчас.
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "HTTP-запросы в обработке.",
		}),
	}
	reg.MustRegister(m.requests, m.duration, m.inFlight)
	return m
}

// statusRecorder запоминает код ответа обработчика.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// Метка маршрута — шаблон `ServeMux` из `r.Pattern`
// (Go 1.23), а не путь: `/orders/1`, `/orders/2`, … дали бы
// по серии на каждый заказ. `since` подменяется в примере,
// чтобы длительности не зависели от машины.
func (m *httpMetrics) middleware(next http.Handler, since func(time.Time) time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		m.duration.WithLabelValues(route).Observe(since(start).Seconds())
		m.requests.WithLabelValues(route, fmt.Sprint(rec.code)).Inc()
	})
}

func main() {
	reg := prometheus.NewRegistry()
	metrics := newHTTPMetrics(reg)
	// Бизнес-метрика — рядом с техническими.
	created := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orders_created_total",
		Help: "Созданные заказы.",
	})
	reg.MustRegister(created)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("qty") == "0" {
			http.Error(w, "bad qty", http.StatusBadRequest)
			return
		}
		created.Inc()
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "o-1" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, `{"id":"o-1"}`)
	})

	// Подменённые часы: длительности запросов по очереди.
	var mu sync.Mutex
	fake := []time.Duration{10 * time.Millisecond, 40 * time.Millisecond, 5 * time.Millisecond, 200 * time.Millisecond, time.Millisecond}
	since := func(time.Time) time.Duration {
		mu.Lock()
		defer mu.Unlock()
		d := fake[0]
		fake = fake[1:]
		return d
	}

	// `/metrics` — отдельный маршрут без middleware: свои
	// скрейпы в RED не считаем.
	root := http.NewServeMux()
	root.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	root.Handle("/", metrics.middleware(mux, since))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: root, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	for _, req := range []struct{ method, path string }{
		{"POST", "/orders"}, {"POST", "/orders?qty=0"},
		{"GET", "/orders/o-1"}, {"GET", "/orders/o-2"}, {"GET", "/admin"},
	} {
		r, _ := http.NewRequest(req.method, base+req.path, nil)
		resp, err := client.Do(r)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
	}

	// Так `/metrics` видит Prometheus (строки `# HELP` убраны
	// для краткости). В реестре только наши метрики; метрики
	// рантайма Go добавляет `collectors.NewGoCollector()`.
	// Сумма `0.20500000000000002` — обычное сложение float64.
	resp, err := client.Get(base + "/metrics")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "# HELP") {
			fmt.Println(line)
		}
	}
}

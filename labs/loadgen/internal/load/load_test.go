package load

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOrders — REST «Заказов» в миниатюре: каталог из двух товаров, заказы
// в памяти, история «с реплики», которая без токена не видит последний
// заказ (как реплика с задержкой).
type fakeOrders struct {
	mu        sync.Mutex
	nextID    int
	orders    map[int64][]string // покупатель → номера заказов
	overload  atomic.Bool        // CreateOrder отвечает 429
	inFlight  atomic.Int64
	maxFlight atomic.Int64
	gate      chan struct{} // не nil — запросы ждут, пока его не закроют
	arrived   chan struct{}
}

func newFake() *fakeOrders { return &fakeOrders{orders: map[int64][]string{}} }

func (f *fakeOrders) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		m := f.maxFlight.Load()
		if n <= m || f.maxFlight.CompareAndSwap(m, n) {
			break
		}
	}
	if f.arrived != nil {
		f.arrived <- struct{}{}
	}
	if f.gate != nil {
		<-f.gate
	}
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/products/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/products/")
		if id != "1" && id != "2" {
			http.Error(w, `{"error":{"code":"NOT_FOUND"}}`, http.StatusNotFound)
			return
		}
		fmt.Fprintf(w, `{"product":{"product_id":"%s","name":"x","price_cents":"100"}}`, id)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/orders":
		if f.overload.Load() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":{"code":"RESOURCE_EXHAUSTED"}}`, http.StatusTooManyRequests)
			return
		}
		var req struct {
			CustomerID int64 `json:"customer_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.nextID++
		id := fmt.Sprint(f.nextID)
		f.orders[req.CustomerID] = append(f.orders[req.CustomerID], id)
		f.mu.Unlock()
		fmt.Fprintf(w, `{"order":{"order_id":"%s"},"consistency_token":"lsn:0/1"}`, id)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/orders":
		var c int64
		fmt.Sscan(r.URL.Query().Get("customer_id"), &c)
		f.mu.Lock()
		list := append([]string(nil), f.orders[c]...)
		f.mu.Unlock()
		if r.URL.Query().Get("consistency_token") == "" && len(list) > 0 {
			list = list[:len(list)-1] // «реплика» отстаёт на последний заказ
		}
		parts := make([]string, len(list))
		for i, id := range list {
			parts[i] = fmt.Sprintf(`{"order_id":"%s"}`, id)
		}
		fmt.Fprintf(w, `{"orders":[%s]}`, strings.Join(parts, ","))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, ":cancel"):
		fmt.Fprint(w, `{"order":{}}`)
	default:
		http.NotFound(w, r)
	}
}

func TestOpenModelRunsTheSchedule(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	stats := NewStats()
	c := NewClient(srv.URL, 5*time.Second, stats)
	mix, err := ParseMix("catalog=3,orders=1,history=1", Scenarios(Options{Customers: 5, Products: 2, MissingRatio: 0.2}, stats))
	if err != nil {
		t.Fatal(err)
	}
	RunOpen(t.Context(), c, mix, OpenConfig{Rate: 400, Duration: 250 * time.Millisecond, Seed: 1}, stats)

	// Расписание: моменты 0, 2.5 мс, 5 мс … < 250 мс — ровно 100 сценариев,
	// сколько бы ни отвечал сервер.
	if stats.Started != 100 || stats.Dropped != 0 {
		t.Fatalf("started %d dropped %d, want 100 and 0", stats.Started, stats.Dropped)
	}
	cat, create, list := stats.Op("catalog.get"), stats.Op("orders.create"), stats.Op("orders.list")
	if cat.Count()+create.Count()+list.Count() != 100 {
		t.Errorf("first requests = %d+%d+%d, want 100", cat.Count(), create.Count(), list.Count())
	}
	if cat.Count() < 40 || create.Count() == 0 || list.Count() == 0 {
		t.Errorf("mix not respected: catalog %d create %d list %d", cat.Count(), create.Count(), list.Count())
	}
	if cat.Outcomes["404"] == 0 || cat.Outcomes["200"] == 0 {
		t.Errorf("catalog outcomes = %v, want both 200 and 404 (missing ratio)", cat.Outcomes)
	}
	if stats.Op("orders.cancel").Count() != create.Count() {
		t.Errorf("cancel %d != create %d: каждый заказ отменяется", stats.Op("orders.cancel").Count(), create.Count())
	}
	var report strings.Builder
	stats.Report(&report)
	for _, want := range []string{"catalog.get", "p99.9", "404:"} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, report.String())
		}
	}
}

// TestOpenModelMeasuresFromSchedule: сервер «завис» и отвечает только
// после команды теста. Задержка каждого запроса — от запланированного
// момента, то есть не меньше, чем прошло от плана до ответа: зависание не
// прячется (нет coordinated omission).
func TestOpenModelMeasuresFromSchedule(t *testing.T) {
	fake := newFake()
	fake.gate = make(chan struct{})
	fake.arrived = make(chan struct{}, 100)
	srv := httptest.NewServer(fake)
	defer srv.Close()
	stats := NewStats()
	c := NewClient(srv.URL, 10*time.Second, stats)
	mix, _ := ParseMix("catalog", Scenarios(Options{Products: 2}, stats))

	start := time.Now()
	done := make(chan struct{})
	go func() {
		RunOpen(context.Background(), c, mix, OpenConfig{Rate: 1000, Duration: 10 * time.Millisecond, Seed: 1}, stats)
		close(done)
	}()
	for range 10 {
		<-fake.arrived
	}
	released := time.Now()
	close(fake.gate)
	<-done

	op := stats.Op("catalog.get")
	if op.Count() != 10 {
		t.Fatalf("requests = %d, want 10", op.Count())
	}
	// Последний запрос запланирован на start+9мс; даже он ждал не меньше
	// released − (start + 9мс). Минимальная задержка (p0 ≈ первая корзина)
	// не может быть меньше.
	floor := released.Sub(start.Add(9 * time.Millisecond))
	if got := op.Quantile(0.0001); got < floor*31/32 {
		t.Errorf("min latency %v < %v: задержка посчитана от отправки, а не от плана", got, floor)
	}
	// А первый запрос (план — start) ждал не меньше released − start.
	if got := op.Latency.Max(); got < released.Sub(start)*31/32 {
		t.Errorf("max latency %v < %v", got, released.Sub(start))
	}
}

func TestOpenModelDropsWhenGeneratorLimitIsReached(t *testing.T) {
	fake := newFake()
	fake.gate = make(chan struct{})
	fake.arrived = make(chan struct{}, 100)
	srv := httptest.NewServer(fake)
	defer srv.Close()
	stats := NewStats()
	c := NewClient(srv.URL, 10*time.Second, stats)
	mix, _ := ParseMix("catalog", Scenarios(Options{Products: 2}, stats))
	done := make(chan struct{})
	go func() {
		RunOpen(context.Background(), c, mix, OpenConfig{Rate: 1000, Duration: 20 * time.Millisecond, MaxInFlight: 2, Seed: 1}, stats)
		close(done)
	}()
	<-fake.arrived
	<-fake.arrived
	// Пока два запроса висят, расписание идёт дальше, а запускать некуда.
	// Опрос, а не пауза: ждём, пока расписание (20 мс) пройдёт целиком.
	for {
		stats.mu.Lock()
		n := stats.Started + stats.Dropped
		stats.mu.Unlock()
		if n >= 20 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(fake.gate)
	<-done
	if stats.Started != 2 || stats.Dropped != 18 {
		t.Errorf("started %d dropped %d, want 2 and 18", stats.Started, stats.Dropped)
	}
}

func TestClosedModelKeepsUsersBound(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	stats := NewStats()
	c := NewClient(srv.URL, 5*time.Second, stats)
	mix, _ := ParseMix("catalog=1,history=1", Scenarios(Options{Customers: 3, Products: 2}, stats))
	RunClosed(t.Context(), c, mix, ClosedConfig{Users: 3, Duration: 100 * time.Millisecond, Seed: 1}, stats)
	if stats.Started < 3 {
		t.Errorf("started = %d, want at least one per user", stats.Started)
	}
	if m := fake.maxFlight.Load(); m > 3 {
		t.Errorf("max concurrent requests = %d, want ≤ 3 users", m)
	}
}

func TestOverloadAndReadYourWrites(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	stats := NewStats()
	c := NewClient(srv.URL, 5*time.Second, stats)
	sc := Scenarios(Options{Customers: 1, RYW: true}, stats)["orders"]
	r := rand.New(rand.NewPCG(1, 1))
	for range 3 {
		sc.Run(t.Context(), c, r, time.Now())
	}
	if stats.RYWChecks != 3 || stats.StaleNoToken != 3 || stats.StaleWithToken != 0 {
		t.Errorf("ryw checks %d, stale without token %d, with token %d; want 3, 3, 0",
			stats.RYWChecks, stats.StaleNoToken, stats.StaleWithToken)
	}
	fake.overload.Store(true)
	sc.Run(t.Context(), c, r, time.Now())
	if got := stats.Op("orders.create").Outcomes["429"]; got != 1 {
		t.Errorf("429 outcomes = %d, want 1", got)
	}
	if got := stats.Op("orders.create").Errors(); got != 1 {
		t.Errorf("create errors = %d, want 1", got)
	}
}

func TestParseMixAndPick(t *testing.T) {
	all := Scenarios(Options{}, NewStats())
	for _, bad := range []string{"", "catalog=-1", "nope=1", "catalog=x", "catalog=0"} {
		if _, err := ParseMix(bad, all); err == nil {
			t.Errorf("ParseMix(%q): want error", bad)
		}
	}
	mix, err := ParseMix("catalog=6, orders=1 ,history=3", all)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(7, 7))
	counts := map[string]int{}
	for range 10000 {
		counts[pick(mix, r).Name]++
	}
	for name, want := range map[string]int{"catalog": 6000, "orders": 1000, "history": 3000} {
		if got := counts[name]; got < want*9/10 || got > want*11/10 {
			t.Errorf("%s picked %d of 10000, want ≈%d", name, got, want)
		}
	}
}

func TestTransportErrorsAreOutcomes(t *testing.T) {
	stats := NewStats()
	c := NewClient("http://127.0.0.1:1", time.Second, stats) // порт 1 закрыт
	mix, _ := ParseMix("catalog", Scenarios(Options{Products: 1}, stats))
	mix[0].Run(t.Context(), c, rand.New(rand.NewPCG(1, 1)), time.Now())
	if got := stats.Op("catalog.get").Outcomes["conn_error"]; got != 1 {
		t.Errorf("outcomes = %v, want conn_error", stats.Op("catalog.get").Outcomes)
	}
}

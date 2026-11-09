package analyze

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gotutor/labs/loadgen/internal/load"
)

// fakeService — «Заказы» для разбора: REST, переключатели и метрики,
// которые реагируют на переключатели (кеш выключен — каталог идёт в базу).
type fakeService struct {
	mu               sync.Mutex
	cache, replica   bool
	replicaAvailable bool
	dbQueries        atomic.Int64
	hits             atomic.Int64
	replicaReads     atomic.Int64
	primaryReads     atomic.Int64
	nextID           atomic.Int64
	changes          []string
}

func (f *fakeService) rest() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {})
	mux.HandleFunc("GET /v1/products/{id}", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		cache := f.cache
		f.mu.Unlock()
		if cache {
			f.hits.Add(1)
		} else {
			f.dbQueries.Add(1)
		}
		fmt.Fprint(w, `{"product":{}}`)
	})
	mux.HandleFunc("POST /v1/orders", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"order":{"order_id":"%d"},"consistency_token":"lsn:0/1"}`, f.nextID.Add(1))
	})
	mux.HandleFunc("POST /v1/orders/{rest}", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{}`) })
	mux.HandleFunc("GET /v1/orders", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		replica := f.replica
		f.mu.Unlock()
		if replica {
			f.replicaReads.Add(1)
		} else {
			f.primaryReads.Add(1)
		}
		fmt.Fprint(w, `{"orders":[]}`)
	})
	return mux
}

func (f *fakeService) ops() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/features", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		fmt.Fprintf(w, `{"catalog_cache":%t,"replica_reads":%t}`, f.cache, f.replica)
	})
	mux.HandleFunc("POST /admin/features", func(w http.ResponseWriter, r *http.Request) {
		var s features
		json.NewDecoder(r.Body).Decode(&s)
		f.mu.Lock()
		defer f.mu.Unlock()
		if s.ReplicaReads != nil && *s.ReplicaReads && !f.replicaAvailable {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if s.CatalogCache != nil {
			f.cache = *s.CatalogCache
		}
		if s.ReplicaReads != nil {
			f.replica = *s.ReplicaReads
		}
		f.changes = append(f.changes, fmt.Sprintf("%t/%t", f.cache, f.replica))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "# TYPE orders_catalog_db_queries_total counter\norders_catalog_db_queries_total %d\n", f.dbQueries.Load())
		fmt.Fprintf(w, "orders_catalog_cache_lookups_total{result=\"hit\"} %d\n", f.hits.Load())
		fmt.Fprintf(w, "orders_history_reads_total{reason=\"no_token\",target=\"replica\"} %d\n", f.replicaReads.Load())
		fmt.Fprintf(w, "orders_history_reads_total{reason=\"disabled\",target=\"primary\"} %d\n", f.primaryReads.Load())
	})
	return mux
}

func runAnalyze(t *testing.T, f *fakeService) string {
	t.Helper()
	rest, ops := httptest.NewServer(f.rest()), httptest.NewServer(f.ops())
	t.Cleanup(rest.Close)
	t.Cleanup(ops.Close)
	var out strings.Builder
	err := Run(t.Context(), Config{
		Target: rest.URL, Ops: ops.URL, Rate: 200, Duration: 150 * time.Millisecond, Warmup: 20 * time.Millisecond,
		Mix: "catalog=6,orders=1,history=3", Options: load.Options{Customers: 10, Products: 3, RYW: true},
		OverloadRate: 100, OverloadDuration: 50 * time.Millisecond,
		Timeout: 2 * time.Second, Seed: 1, Out: &out, ReadyTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestAnalyzeSteps(t *testing.T) {
	f := &fakeService{cache: true, replica: true, replicaAvailable: true}
	out := runAnalyze(t, f)
	for _, want := range []string{"шаг 1 база", "шаг 2 +кеш", "шаг 3 +реплика", "шаг 4 перегрузка", "══ сводка ══", "Как читать"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Переключатели менялись по шагам и вернулись в исходное состояние.
	f.mu.Lock()
	changes := strings.Join(f.changes, " ")
	cache, replica := f.cache, f.replica
	f.mu.Unlock()
	if !strings.HasPrefix(changes, "false/false true/false true/true") {
		t.Errorf("feature changes = %s", changes)
	}
	if !cache || !replica {
		t.Errorf("features not restored: cache %t replica %t", cache, replica)
	}
	// В сводке: в шаге «база» запросы каталога идут в базу, с кешем — нет.
	var base, cached string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "1 база ") {
			base = line
		}
		if strings.HasPrefix(line, "2 +кеш ") {
			cached = line
		}
	}
	if base == "" || cached == "" || !strings.Contains(cached, " 0.0 ") || strings.Contains(base, " 0.0 ") {
		t.Errorf("summary rows:\n%s\n%s", base, cached)
	}
}

func TestAnalyzeSkipsUnconfiguredReplica(t *testing.T) {
	f := &fakeService{replicaAvailable: false}
	out := runAnalyze(t, f)
	if !strings.Contains(out, "3 +реплика    пропущен") || !strings.Contains(out, "шаг 4 перегрузка") || strings.Contains(out, "4 перегрузка  пропущен") {
		t.Errorf("replica step not skipped:\n%s", out)
	}
}

func TestAnalyzeNeedsAdminEndpoint(t *testing.T) {
	rest := httptest.NewServer((&fakeService{}).rest())
	defer rest.Close()
	ops := httptest.NewServer(http.NotFoundHandler())
	defer ops.Close()
	var out strings.Builder
	err := Run(t.Context(), Config{Target: rest.URL, Ops: ops.URL, Rate: 1, Duration: time.Millisecond, Out: &out, ReadyTimeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "ORDERS_ADMIN_FEATURES") {
		t.Errorf("err = %v, want a hint about ORDERS_ADMIN_FEATURES", err)
	}
}

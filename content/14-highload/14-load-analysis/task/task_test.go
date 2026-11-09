package task

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// Снимки /metrics «Заказов» до и после шага «+кеш, +реплика» (сокращены).
const stepBefore = `# HELP orders_catalog_db_queries_total Catalog queries that reached the database.
# TYPE orders_catalog_db_queries_total counter
orders_catalog_db_queries_total 2590
# HELP orders_catalog_cache_lookups_total Catalog cache lookups per product key.
# TYPE orders_catalog_cache_lookups_total counter
orders_catalog_cache_lookups_total{result="bypass"} 2544
orders_catalog_cache_lookups_total{result="hit"} 4500
orders_catalog_cache_lookups_total{result="miss"} 46
orders_catalog_cache_lookups_total{result="negative_hit"} 20
# TYPE orders_history_reads_total counter
orders_history_reads_total{reason="disabled",target="primary"} 3636
# TYPE grpc_server_handled_total counter
grpc_server_handled_total{grpc_code="OK",grpc_method="CreateOrder",grpc_service="orders.v1.OrderService"} 2346
grpc_server_handled_total{grpc_code="OK",grpc_method="GetProduct",grpc_service="orders.v1.OrderService"} 4584
`

const stepAfter = `# HELP orders_catalog_db_queries_total Catalog queries that reached the database.
# TYPE orders_catalog_db_queries_total counter
orders_catalog_db_queries_total 2670
# TYPE orders_catalog_cache_lookups_total counter
orders_catalog_cache_lookups_total{result="bypass"} 2544
orders_catalog_cache_lookups_total{result="hit"} 5996
orders_catalog_cache_lookups_total{result="miss"} 126
orders_catalog_cache_lookups_total{result="negative_hit"} 21
# TYPE orders_history_reads_total counter
orders_history_reads_total{reason="disabled",target="primary"} 3636
orders_history_reads_total{reason="lagging",target="primary"} 238
orders_history_reads_total{reason="no_token",target="replica"} 911
# TYPE grpc_server_handled_total counter
grpc_server_handled_total{grpc_code="OK",grpc_method="CreateOrder",grpc_service="orders.v1.OrderService"} 2584
grpc_server_handled_total{grpc_code="OK",grpc_method="GetProduct",grpc_service="orders.v1.OrderService"} 6080
`

func mustParse(t *testing.T, s string) []Sample {
	t.Helper()
	out, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestParseSamples(t *testing.T) {
	got := mustParse(t, stepBefore)
	if len(got) != 8 {
		t.Fatalf("Parse: %d samples, want 8 (comments are skipped)", len(got))
	}
	first := got[0]
	if first.Name != "orders_catalog_db_queries_total" || first.Value != 2590 || first.Labels == nil || len(first.Labels) != 0 {
		t.Errorf("first sample = %+v, want orders_catalog_db_queries_total 2590 with empty non-nil Labels", first)
	}
	hit := got[2]
	if hit.Name != "orders_catalog_cache_lookups_total" || hit.Labels["result"] != "hit" || hit.Value != 4500 {
		t.Errorf("third sample = %+v, want lookups{result=hit} 4500", hit)
	}
	last := got[7]
	want := map[string]string{"grpc_code": "OK", "grpc_method": "GetProduct", "grpc_service": "orders.v1.OrderService"}
	if len(last.Labels) != len(want) {
		t.Fatalf("last sample labels = %v, want %v", last.Labels, want)
	}
	for k, v := range want {
		if last.Labels[k] != v {
			t.Errorf("last sample label %s = %q, want %q", k, last.Labels[k], v)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, in := range []string{
		"orders_total\n",              // нет значения
		"orders_total abc\n",          // значение не число
		`orders_total{result="hit} 1`, // незакрытая кавычка
	} {
		if _, err := Parse(strings.NewReader(in)); !errors.Is(err, ErrSyntax) {
			t.Errorf("Parse(%q) error = %v, want ErrSyntax", in, err)
		}
	}
}

func TestIncrease(t *testing.T) {
	before, after := mustParse(t, stepBefore), mustParse(t, stepAfter)
	cases := []struct {
		name  string
		match map[string]string
		want  float64
	}{
		{"orders_catalog_db_queries_total", nil, 80},
		{"orders_catalog_cache_lookups_total", map[string]string{"result": "hit"}, 1496},
		{"orders_catalog_cache_lookups_total", nil, 1577},                           // все ряды
		{"orders_history_reads_total", map[string]string{"target": "replica"}, 911}, // ряд появился за шаг
		{"orders_history_reads_total", map[string]string{"target": "primary"}, 238}, // disabled не менялся
		{"grpc_server_handled_total", map[string]string{"grpc_method": "CreateOrder"}, 238},
		{"orders_shed_rejected_total", nil, 0}, // таких рядов нет
	}
	for _, c := range cases {
		if got := Increase(before, after, c.name, c.match); !near(got, c.want) {
			t.Errorf("Increase(%s, %v) = %v, want %v", c.name, c.match, got, c.want)
		}
	}
}

func TestSummarize(t *testing.T) {
	s, err := Summarize(mustParse(t, stepBefore), mustParse(t, stepAfter), 15)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	want := Summary{
		DBQueries: 80, DBPerSecond: 80.0 / 15,
		CacheHits: 1497, CacheLookups: 1577, HitRatio: 1497.0 / 1577,
		ReplicaReads: 911, HistoryReads: 1149, ReplicaRatio: 911.0 / 1149,
		CreateCalls: 238, Shed: 0, ShedRatio: 0,
	}
	checkSummary(t, s, want)
}

func checkSummary(t *testing.T, got, want Summary) {
	t.Helper()
	pairs := []struct {
		name      string
		got, want float64
	}{
		{"DBQueries", got.DBQueries, want.DBQueries},
		{"DBPerSecond", got.DBPerSecond, want.DBPerSecond},
		{"CacheHits", got.CacheHits, want.CacheHits},
		{"CacheLookups", got.CacheLookups, want.CacheLookups},
		{"HitRatio", got.HitRatio, want.HitRatio},
		{"ReplicaReads", got.ReplicaReads, want.ReplicaReads},
		{"HistoryReads", got.HistoryReads, want.HistoryReads},
		{"ReplicaRatio", got.ReplicaRatio, want.ReplicaRatio},
		{"CreateCalls", got.CreateCalls, want.CreateCalls},
		{"Shed", got.Shed, want.Shed},
		{"ShedRatio", got.ShedRatio, want.ShedRatio},
	}
	for _, p := range pairs {
		if !near(p.got, p.want) {
			t.Errorf("Summary.%s = %v, want %v", p.name, p.got, p.want)
		}
	}
}

package task

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Hash Join, у которого внешняя сторона не вернула строк: хеш
// и скан customers не выполнялись (never executed).
const neverExecutedPlan = `[
  {
    "Plan": {
      "Node Type": "Hash Join",
      "Plan Rows": 3,
      "Actual Rows": 0.00,
      "Actual Loops": 1,
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Relation Name": "orders",
          "Plan Rows": 2,
          "Actual Rows": 0.00,
          "Actual Loops": 1,
          "Rows Removed by Filter": 20000
        },
        {
          "Node Type": "Hash",
          "Plan Rows": 1000,
          "Actual Rows": 0,
          "Actual Loops": 0,
          "Plans": [
            {
              "Node Type": "Seq Scan",
              "Relation Name": "customers",
              "Plan Rows": 1000000,
              "Actual Rows": 0,
              "Actual Loops": 0
            }
          ]
        }
      ]
    }
  }
]`

// Внутренний Seq Scan вложенного цикла: на каждый из 50 циклов
// 10 строк и 990 отброшенных — всего просмотрено 50 000.
const loopsPlan = `[
  {
    "Plan": {
      "Node Type": "Nested Loop",
      "Plan Rows": 400,
      "Actual Rows": 500.00,
      "Actual Loops": 1,
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Relation Name": "customers",
          "Plan Rows": 50,
          "Actual Rows": 50.00,
          "Actual Loops": 1
        },
        {
          "Node Type": "Materialize",
          "Plan Rows": 1000,
          "Actual Rows": 1000.00,
          "Actual Loops": 50,
          "Plans": [
            {
              "Node Type": "Seq Scan",
              "Relation Name": "orders",
              "Plan Rows": 1,
              "Actual Rows": 10.00,
              "Actual Loops": 50,
              "Rows Removed by Filter": 990
            }
          ]
        }
      ]
    }
  }
]`

// Параллельный скан: в JSON тот же "Seq Scan" с "Parallel Aware",
// строки — среднее на процесс (2 воркера + лидер = 3 цикла).
const parallelPlan = `[
  {
    "Plan": {
      "Node Type": "Gather",
      "Plan Rows": 100,
      "Actual Rows": 0.50,
      "Actual Loops": 1,
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Parent Relationship": "Outer",
          "Parallel Aware": true,
          "Relation Name": "events",
          "Plan Rows": 42,
          "Actual Rows": 0.33,
          "Actual Loops": 3,
          "Rows Removed by Filter": 333333
        }
      ]
    }
  }
]`

// Обычный EXPLAIN без ANALYZE: фактических чисел нет.
const plainPlan = `[
  {
    "Plan": {
      "Node Type": "Seq Scan",
      "Relation Name": "orders",
      "Startup Cost": 0.00,
      "Total Cost": 359.00,
      "Plan Rows": 40,
      "Plan Width": 12,
      "Filter": "(total = 7)"
    }
  }
]`

func analyze(t *testing.T, plan string, opts Options) []Finding {
	t.Helper()
	got, err := Analyze([]byte(plan), opts)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	return got
}

func TestNeverExecutedSkipped(t *testing.T) {
	got := analyze(t, neverExecutedPlan, Options{MinSeqScanRows: 10000, MisestimateFactor: 10})
	want := []Finding{{Kind: "seq-scan", Node: "Seq Scan", Relation: "orders", Estimated: 2, Actual: 0}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestLoopsMultiply(t *testing.T) {
	got := analyze(t, loopsPlan, Options{MinSeqScanRows: 20000, MisestimateFactor: 5})
	want := []Finding{
		{Kind: "seq-scan", Node: "Seq Scan", Relation: "orders", Estimated: 1, Actual: 10},
		{Kind: "misestimate", Node: "Seq Scan", Relation: "orders", Estimated: 1, Actual: 10},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestPreOrder(t *testing.T) {
	got := analyze(t, loopsPlan, Options{MinSeqScanRows: 50, MisestimateFactor: 1.2})
	want := []Finding{
		{Kind: "misestimate", Node: "Nested Loop", Estimated: 400, Actual: 500},
		{Kind: "seq-scan", Node: "Seq Scan", Relation: "customers", Estimated: 50, Actual: 50},
		{Kind: "seq-scan", Node: "Seq Scan", Relation: "orders", Estimated: 1, Actual: 10},
		{Kind: "misestimate", Node: "Seq Scan", Relation: "orders", Estimated: 1, Actual: 10},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParallelAndFractions(t *testing.T) {
	got := analyze(t, parallelPlan, Options{MinSeqScanRows: 1000000, MisestimateFactor: 40})
	// (0.33 + 333333) * 3 = 999999.99 — чуть меньше миллиона.
	want := []Finding{
		{Kind: "misestimate", Node: "Gather", Estimated: 100, Actual: 0.5},
		{Kind: "misestimate", Node: "Seq Scan", Relation: "events", Estimated: 42, Actual: 0.33},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	got = analyze(t, parallelPlan, Options{MinSeqScanRows: 999999})
	if len(got) != 1 || got[0].Kind != "seq-scan" || got[0].Relation != "events" {
		t.Fatalf("параллельный Seq Scan: got %+v", got)
	}
}

func TestDisabledChecks(t *testing.T) {
	for _, opts := range []Options{{}, {MinSeqScanRows: -1, MisestimateFactor: -1}} {
		for _, plan := range []string{seqScanPlan, nestedLoopPlan, loopsPlan, parallelPlan} {
			if got := analyze(t, plan, opts); len(got) != 0 {
				t.Fatalf("opts %+v: got %+v, want ничего", opts, got)
			}
		}
	}
	got := analyze(t, plainPlan, Options{MinSeqScanRows: 1, MisestimateFactor: 1})
	if len(got) != 0 {
		t.Fatalf("EXPLAIN без ANALYZE: got %+v, want ничего", got)
	}
}

func TestBadPlanShapes(t *testing.T) {
	for _, in := range []string{`{}`, `[{}]`, `[{"Plan": null}]`, `[{"Plan": "Seq Scan"}]`, `null`} {
		if got, err := Analyze([]byte(in), Options{MinSeqScanRows: 1}); !errors.Is(err, ErrBadPlan) || got != nil {
			t.Errorf("Analyze(%s) = (%+v, %v), want (nil, ErrBadPlan)", in, got, err)
		}
	}
}

// TestPostgresPlan — на настоящей базе профиля labs: без индекса
// фильтр по большой таблице — Seq Scan, с индексом — находок нет.
func TestPostgresPlan(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN не задан — нужен профиль labs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, q := range []string{
		`SET max_parallel_workers_per_gather = 0`,
		`DROP TABLE IF EXISTS events`,
		`CREATE TABLE events (id int PRIMARY KEY, kind int NOT NULL)`,
		`INSERT INTO events SELECT g, g % 100 FROM generate_series(1, 50000) g`,
		`ANALYZE events`,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	plan := func() []byte {
		var raw json.RawMessage
		err := conn.QueryRow(ctx, `EXPLAIN (ANALYZE, FORMAT JSON) SELECT id FROM events WHERE kind = $1`, 42).Scan(&raw)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	opts := Options{MinSeqScanRows: 10000, MisestimateFactor: 10}
	got, err := Analyze(plan(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "seq-scan" || got[0].Relation != "events" || got[0].Actual != 500 {
		t.Fatalf("без индекса: got %+v, want один seq-scan по events с Actual 500", got)
	}
	if _, err := conn.Exec(ctx, `CREATE INDEX events_kind ON events (kind)`); err != nil {
		t.Fatal(err)
	}
	if got, err = Analyze(plan(), opts); err != nil || len(got) != 0 {
		t.Fatalf("с индексом: (%+v, %v), want ничего", got, err)
	}
}

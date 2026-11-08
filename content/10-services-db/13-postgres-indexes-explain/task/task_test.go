package task

import (
	"errors"
	"slices"
	"testing"
)

// Планы — настоящий вывод EXPLAIN (ANALYZE, FORMAT JSON) из
// PostgreSQL 18, сокращённый до нужных полей. В 18-й версии
// "Actual Rows" — дробное число (среднее на цикл).
const seqScanPlan = `[
  {
    "Plan": {
      "Node Type": "Seq Scan",
      "Parallel Aware": false,
      "Relation Name": "orders",
      "Alias": "orders",
      "Startup Cost": 0.00,
      "Total Cost": 359.00,
      "Plan Rows": 40,
      "Plan Width": 12,
      "Actual Rows": 40.00,
      "Actual Loops": 1,
      "Filter": "(total = 7)",
      "Rows Removed by Filter": 19960
    },
    "Planning Time": 0.051,
    "Triggers": [],
    "Execution Time": 1.342
  }
]`

const nestedLoopPlan = `[
  {
    "Plan": {
      "Node Type": "Nested Loop",
      "Join Type": "Inner",
      "Plan Rows": 1,
      "Actual Rows": 80.00,
      "Actual Loops": 1,
      "Plans": [
        {
          "Node Type": "Index Scan",
          "Parent Relationship": "Outer",
          "Index Name": "customers_pkey",
          "Relation Name": "customers",
          "Plan Rows": 5,
          "Actual Rows": 4.00,
          "Actual Loops": 1,
          "Rows Removed by Filter": 45
        },
        {
          "Node Type": "Bitmap Heap Scan",
          "Parent Relationship": "Inner",
          "Relation Name": "orders",
          "Plan Rows": 20,
          "Actual Rows": 20.00,
          "Actual Loops": 4,
          "Plans": [
            {
              "Node Type": "Bitmap Index Scan",
              "Index Name": "orders_customer_id_idx",
              "Plan Rows": 20,
              "Actual Rows": 20.00,
              "Actual Loops": 4
            }
          ]
        }
      ]
    }
  }
]`

func TestSeqScan(t *testing.T) {
	got, err := Analyze([]byte(seqScanPlan), Options{MinSeqScanRows: 10000, MisestimateFactor: 10})
	want := []Finding{{Kind: "seq-scan", Node: "Seq Scan", Relation: "orders", Estimated: 40, Actual: 40}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Analyze = (%+v, %v)\nwant %+v", got, err, want)
	}
	// Просмотрено 40 + 19960 = 20000 строк — ниже порога находки нет.
	got, err = Analyze([]byte(seqScanPlan), Options{MinSeqScanRows: 20001})
	if err != nil || len(got) != 0 {
		t.Fatalf("Analyze с порогом 20001 = (%+v, %v), want ничего", got, err)
	}
}

func TestMisestimate(t *testing.T) {
	got, err := Analyze([]byte(nestedLoopPlan), Options{MinSeqScanRows: 1000, MisestimateFactor: 10})
	want := []Finding{{Kind: "misestimate", Node: "Nested Loop", Relation: "", Estimated: 1, Actual: 80}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Analyze = (%+v, %v)\nwant %+v", got, err, want)
	}
}

func TestBadPlan(t *testing.T) {
	for _, in := range []string{``, `not json`, `[]`} {
		if _, err := Analyze([]byte(in), Options{MinSeqScanRows: 1}); !errors.Is(err, ErrBadPlan) {
			t.Errorf("Analyze(%q) err = %v, want ErrBadPlan", in, err)
		}
	}
}

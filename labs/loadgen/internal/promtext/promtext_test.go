package promtext

import (
	"strings"
	"testing"
)

const sample = `# HELP orders_history_reads_total History reads.
# TYPE orders_history_reads_total counter
orders_history_reads_total{reason="no_token",target="replica"} 12
orders_history_reads_total{reason="lagging",target="primary"} 3
orders_history_reads_total{reason="caught_up",target="replica"} 5 1700000000000
orders_catalog_db_queries_total 42
weird{msg="a \"quoted\", value",x="1"} 1.5e+02
go_gc_duration_seconds{quantile="0.5"} NaN
`

func TestParseAndSum(t *testing.T) {
	snap, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Sum("orders_history_reads_total", map[string]string{"target": "replica"}); got != 17 {
		t.Errorf("replica reads = %v, want 17", got)
	}
	if got := snap.Sum("orders_history_reads_total", nil); got != 20 {
		t.Errorf("all reads = %v, want 20", got)
	}
	if got := snap.Sum("orders_catalog_db_queries_total", nil); got != 42 {
		t.Errorf("db queries = %v", got)
	}
	if got := snap.Sum("weird", map[string]string{"msg": `a "quoted", value`}); got != 150 {
		t.Errorf("escaped label value: %v", got)
	}
	if !snap.Has("orders_catalog_db_queries_total") || snap.Has("absent") {
		t.Error("Has")
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"novalue", `m{a="1" 1`, "m{a=1} 2", "m notanumber"} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("Parse(%q): want error", bad)
		}
	}
}

// Прирост считается по рядам: сброс одного ряда (рестарт) не прячется за
// ростом другого, новый ряд считается от нуля.
func TestIncrease(t *testing.T) {
	before, err := Parse(strings.NewReader(`x{a="1"} 100
x{a="2"} 10
`))
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse(strings.NewReader(`x{a="1"} 5
x{a="2"} 30
x{a="3"} 7
y 1
`))
	if err != nil {
		t.Fatal(err)
	}
	// a=1: сброс → 5; a=2: 20; a=3: новый → 7. Разность сумм дала бы 0.
	if got := after.Increase(before, "x", nil); got != 32 {
		t.Errorf("Increase = %v, want 32", got)
	}
	if got := after.Increase(before, "x", map[string]string{"a": "2"}); got != 20 {
		t.Errorf("Increase{a=2} = %v, want 20", got)
	}
	if got := after.Increase(before, "nope", nil); got != 0 {
		t.Errorf("Increase(nope) = %v, want 0", got)
	}
}

package task

import (
	"bytes"
	"runtime"
	"runtime/debug"
	"testing"
)

// quiet делает пул и счёт аллокаций предсказуемыми: один P
// и никаких автоматических сборок мусора, которые очищают пул.
func quiet(t *testing.T) {
	t.Helper()
	procs := runtime.GOMAXPROCS(1)
	gc := debug.SetGCPercent(-1)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(procs)
		debug.SetGCPercent(gc)
	})
}

// countingWriter запоминает, сколько раз его вызвали и что записали.
type countingWriter struct {
	calls int
	buf   bytes.Buffer
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.calls++
	return w.buf.Write(p)
}

func TestAppendOrder(t *testing.T) {
	o := Order{ID: 1, Customer: "ann", Items: []Item{{SKU: "A-1", Qty: 2, PriceCents: 1999}}, Paid: true}
	got := string(AppendOrder(nil, &o))
	want := `{"id":1,"customer":"ann","items":[{"sku":"A-1","qty":2,"price":1999}],"paid":true,"total":3998}`
	if got != want {
		t.Fatalf("AppendOrder:\n got %s\nwant %s", got, want)
	}
}

func TestAppendOrderEscaping(t *testing.T) {
	o := Order{ID: 7, Customer: "Анна \"VIP\" \\ <b>\n"}
	got := string(AppendOrder([]byte("prefix:"), &o))
	want := `prefix:{"id":7,"customer":"Анна \"VIP\" \\ <b>\u000a","items":[],"paid":false,"total":0}`
	if got != want {
		t.Fatalf("AppendOrder:\n got %s\nwant %s", got, want)
	}
}

func TestWriteOrders(t *testing.T) {
	orders := []Order{
		{ID: 1, Customer: "a", Items: []Item{{SKU: "X", Qty: 1, PriceCents: 100}, {SKU: "Y", Qty: 3, PriceCents: 5}}},
		{ID: 2, Customer: "b", Paid: true},
	}
	var w countingWriter
	if err := WriteOrders(&w, orders); err != nil {
		t.Fatal(err)
	}
	want := `[{"id":1,"customer":"a","items":[{"sku":"X","qty":1,"price":100},{"sku":"Y","qty":3,"price":5}],"paid":false,"total":115},` +
		`{"id":2,"customer":"b","items":[],"paid":true,"total":0}]` + "\n"
	if w.buf.String() != want {
		t.Fatalf("WriteOrders:\n got %q\nwant %q", w.buf.String(), want)
	}
	if w.calls != 1 {
		t.Fatalf("Write вызван %d раз, want 1", w.calls)
	}
}

func TestAppendOrderNoAllocs(t *testing.T) {
	quiet(t)
	o := Order{ID: 42, Customer: "Анна", Items: []Item{{"A-1", 2, 1999}, {"B-7", 1, 54900}}, Paid: true}
	buf := make([]byte, 0, 4096)
	allocs := testing.AllocsPerRun(1000, func() {
		buf = AppendOrder(buf[:0], &o)
	})
	if len(buf) == 0 {
		t.Fatal("AppendOrder ничего не дописал")
	}
	if allocs != 0 {
		t.Fatalf("AppendOrder при достаточной ёмкости: %.0f аллокаций на вызов, want 0", allocs)
	}
}

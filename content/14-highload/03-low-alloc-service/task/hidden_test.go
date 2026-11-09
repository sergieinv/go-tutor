package task

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

type discard struct{ calls, bytes int }

func (d *discard) Write(p []byte) (int, error) {
	d.calls++
	d.bytes += len(p)
	return len(p), nil
}

type failing struct{}

var errBoom = errors.New("boom")

func (failing) Write(p []byte) (int, error) { return 0, errBoom }

func TestNumbersAndBool(t *testing.T) {
	o := Order{
		ID:       math.MinInt64,
		Customer: "",
		Items:    []Item{{SKU: "N", Qty: -3, PriceCents: 7}, {SKU: "Z", Qty: 0, PriceCents: math.MaxInt64}},
	}
	got := string(AppendOrder(nil, &o))
	want := `{"id":-9223372036854775808,"customer":"","items":[{"sku":"N","qty":-3,"price":7},{"sku":"Z","qty":0,"price":9223372036854775807}],"paid":false,"total":-21}`
	if got != want {
		t.Fatalf("AppendOrder:\n got %s\nwant %s", got, want)
	}
}

func TestTotalOverflowWraps(t *testing.T) {
	o := Order{ID: 1, Items: []Item{{SKU: "a", Qty: 2, PriceCents: math.MaxInt64}}}
	got := string(AppendOrder(nil, &o))
	if !strings.HasSuffix(got, `"total":-2}`) {
		t.Fatalf("переполнение total должно заворачиваться как int64: %s", got)
	}
}

func TestControlBytes(t *testing.T) {
	o := Order{ID: 3, Customer: "\x00\x1f\x7f\t\r", Items: []Item{{SKU: `a"b\c`}}}
	got := string(AppendOrder(nil, &o))
	want := `{"id":3,"customer":"\u0000\u001f` + "\x7f" + `\u0009\u000d","items":[{"sku":"a\"b\\c","qty":0,"price":0}],"paid":false,"total":0}`
	if got != want {
		t.Fatalf("AppendOrder:\n got %q\nwant %q", got, want)
	}
}

func TestPrefixPreservedOnGrowth(t *testing.T) {
	dst := make([]byte, 3, 3)
	copy(dst, "abc")
	got := AppendOrder(dst, &Order{ID: 9, Items: []Item{}})
	if string(got) != `abc{"id":9,"customer":"","items":[],"paid":false,"total":0}` {
		t.Fatalf("AppendOrder(\"abc\") = %s", got)
	}
	if string(dst) != "abc" {
		t.Fatalf("исходный dst изменён: %q", dst)
	}
}

// Ответ — валидный JSON, который encoding/json читает обратно
// в те же значения: на псевдослучайных заказах с фиксированным seed.
func TestRoundTripRandom(t *testing.T) {
	type itemJSON struct {
		SKU   string `json:"sku"`
		Qty   int    `json:"qty"`
		Price int64  `json:"price"`
	}
	type orderJSON struct {
		ID       int64      `json:"id"`
		Customer string     `json:"customer"`
		Items    []itemJSON `json:"items"`
		Paid     bool       `json:"paid"`
		Total    int64      `json:"total"`
	}
	r := rand.New(rand.NewPCG(14, 3))
	alphabet := []string{"a", "Z", "0", " ", `"`, `\`, "\n", "\t", "\x01", "\x1f", "<", "&", "é", "Ж", "🙂", "/"}
	str := func() string {
		var sb strings.Builder
		for range r.IntN(12) {
			sb.WriteString(alphabet[r.IntN(len(alphabet))])
		}
		return sb.String()
	}
	for i := range 300 {
		o := Order{ID: r.Int64() - r.Int64(), Customer: str(), Paid: r.IntN(2) == 1}
		var total int64
		for range r.IntN(4) {
			it := Item{SKU: str(), Qty: r.IntN(200) - 100, PriceCents: r.Int64N(1_000_000)}
			total += int64(it.Qty) * it.PriceCents
			o.Items = append(o.Items, it)
		}
		b := AppendOrder(nil, &o)
		var got orderJSON
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("заказ %d: невалидный JSON %s: %v", i, b, err)
		}
		if got.ID != o.ID || got.Customer != o.Customer || got.Paid != o.Paid || got.Total != total || len(got.Items) != len(o.Items) {
			t.Fatalf("заказ %d: прочитано %+v из %s", i, got, b)
		}
		for j, it := range o.Items {
			if g := got.Items[j]; g.SKU != it.SKU || g.Qty != it.Qty || g.Price != it.PriceCents {
				t.Fatalf("заказ %d, позиция %d: прочитано %+v из %s", i, j, g, b)
			}
		}
	}
}

func TestWriteOrdersEmpty(t *testing.T) {
	for _, orders := range [][]Order{nil, {}} {
		var w countingWriter
		if err := WriteOrders(&w, orders); err != nil {
			t.Fatal(err)
		}
		if w.buf.String() != "[]\n" || w.calls != 1 {
			t.Fatalf("WriteOrders(%v): %q за %d вызовов, want \"[]\\n\" за 1", orders, w.buf.String(), w.calls)
		}
	}
}

func TestWriteOrdersError(t *testing.T) {
	err := WriteOrders(failing{}, []Order{{ID: 1}})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want ошибку Write", err)
	}
}

// Ответы разного размера подряд: буфер из пула переиспользуется,
// и остатки прошлого ответа не попадают в следующий.
func TestWriteOrdersReuseIsClean(t *testing.T) {
	big := make([]Order, 2000)
	for i := range big {
		big[i] = Order{ID: int64(i), Customer: strings.Repeat("x", 30)}
	}
	small := []Order{{ID: 5, Customer: "s"}}
	want := `[{"id":5,"customer":"s","items":[],"paid":false,"total":0}]` + "\n"
	for range 3 {
		var wb, ws countingWriter
		if err := WriteOrders(&wb, big); err != nil {
			t.Fatal(err)
		}
		if wb.calls != 1 || !strings.HasPrefix(wb.buf.String(), `[{"id":0,`) || !strings.HasSuffix(wb.buf.String(), `"total":0}]`+"\n") {
			t.Fatalf("большой ответ: %d вызовов, длина %d", wb.calls, wb.buf.Len())
		}
		if err := WriteOrders(&ws, small); err != nil {
			t.Fatal(err)
		}
		if ws.buf.String() != want {
			t.Fatalf("после большого ответа маленький = %q, want %q", ws.buf.String(), want)
		}
	}
}

func TestWriteOrdersNoAllocs(t *testing.T) {
	quiet(t)
	orders := []Order{
		{ID: 1, Customer: "Анна", Items: []Item{{"A-1", 2, 1999}, {"B-7", 1, 54900}}, Paid: true},
		{ID: 2, Customer: "Борис", Items: []Item{{"C-3", 5, 10}}},
		{ID: 3, Customer: "Вера"},
	}
	w := &discard{}
	WriteOrders(w, orders) // прогрев: первый буфер создаёт New
	allocs := testing.AllocsPerRun(1000, func() {
		if err := WriteOrders(w, orders); err != nil {
			t.Fatal(err)
		}
	})
	if w.calls == 0 || w.bytes == 0 {
		t.Fatal("WriteOrders ничего не записал")
	}
	if allocs != 0 {
		t.Fatalf("WriteOrders в установившемся режиме: %.0f аллокаций на вызов, want 0 — буфер из sync.Pool?", allocs)
	}
}

package task

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
)

func ExampleFormatMoney() {
	fmt.Println(FormatMoney(-5))
	fmt.Println(FormatMoney(100))
	fmt.Println(FormatMoney(math.MaxInt64))
	fmt.Println(FormatMoney(math.MinInt64))
	// Output:
	// -0.05
	// 1.00
	// 92233720368547758.07
	// -92233720368547758.08
}

// Ширина колонки названий считается в символах, а не в байтах.
func ExampleWriteReceipt_cyrillic() {
	WriteReceipt(os.Stdout, []Item{
		{Name: "Молоко", Qty: 2, Price: 8990},
		{Name: "Хлеб", Qty: 1, Price: 4500},
		{Name: "Сыр", Qty: 1, Price: 125000},
	})
	// Output:
	// Молоко ×2   179.80
	// Хлеб         45.00
	// Сыр        1250.00
	// Итого      1474.80
}

func ExampleWriteReceipt_empty() {
	WriteReceipt(os.Stdout, nil)
	// Output:
	// Итого  0.00
}

func ExampleWriteReceipt_longName() {
	WriteReceipt(os.Stdout, []Item{
		{Name: "Подарочный сертификат", Qty: 1, Price: 0},
		{Name: "Чай", Qty: 12, Price: 99},
	})
	// Output:
	// Подарочный сертификат   0.00
	// Чай ×12                11.88
	// Итого                  11.88
}

func ExampleWriteReceipt_mixed() {
	WriteReceipt(os.Stdout, []Item{
		{Name: "go", Qty: 3, Price: 7},
		{Name: "Ёж", Qty: 1, Price: 1000000},
	})
	// Output:
	// go ×3      0.21
	// Ёж     10000.00
	// Итого  10000.21
}

func TestWriteReceiptInvalid(t *testing.T) {
	for _, items := range [][]Item{
		{{Name: "", Qty: 1, Price: 100}},
		{{Name: "a", Qty: 0, Price: 100}},
		{{Name: "a", Qty: -1, Price: 100}},
		{{Name: "a", Qty: 1, Price: -1}},
		{{Name: "ok", Qty: 1, Price: 1}, {Name: "bad", Qty: 0, Price: 1}},
	} {
		var buf bytes.Buffer
		err := WriteReceipt(&buf, items)
		if !errors.Is(err, ErrInvalidItem) {
			t.Errorf("WriteReceipt(%+v) = %v, want ErrInvalidItem", items, err)
		}
		if buf.Len() != 0 {
			t.Errorf("WriteReceipt(%+v) при ошибке записал %q, want ничего", items, buf.String())
		}
	}
}

// failWriter принимает first успешных Write, затем возвращает ошибку
// и запоминает, сколько раз его вызвали.
type failWriter struct {
	first int
	calls int
	err   error
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.first {
		return 0, w.err
	}
	return len(p), nil
}

func TestWriteReceiptWriterError(t *testing.T) {
	items := []Item{{Name: "a", Qty: 1, Price: 1}, {Name: "b", Qty: 1, Price: 2}, {Name: "c", Qty: 1, Price: 3}}
	for _, first := range []int{0, 1} {
		boom := fmt.Errorf("диск полон (%d)", first)
		w := &failWriter{first: first, err: boom}
		err := WriteReceipt(w, items)
		if w.calls <= first {
			// Весь чек уместился в меньшее число вызовов — ошибка не наступила.
			if err != nil {
				t.Errorf("WriteReceipt вернул %v без ошибки записи", err)
			}
			continue
		}
		if !errors.Is(err, boom) {
			t.Errorf("WriteReceipt при ошибке записи вернул %v, want %v", err, boom)
		}
		if w.calls != first+1 {
			t.Errorf("после ошибки записи WriteReceipt продолжил писать: %d вызовов Write, want %d", w.calls, first+1)
		}
	}
}

func TestWriteReceiptBuffer(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReceipt(&buf, []Item{{Name: "x", Qty: 1, Price: 1}}); err != nil {
		t.Fatalf("WriteReceipt: %v", err)
	}
	want := "x      0.01\nИтого  0.01\n"
	if buf.String() != want {
		t.Errorf("WriteReceipt = %q, want %q", buf.String(), want)
	}
}

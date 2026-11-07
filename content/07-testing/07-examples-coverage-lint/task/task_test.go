package task

import (
	"os"
	"testing"
)

func TestFormatMoney(t *testing.T) {
	tests := []struct {
		kop  int64
		want string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{1234, "12.34"},
		{-150, "-1.50"},
	}
	for _, tt := range tests {
		if got := FormatMoney(tt.kop); got != tt.want {
			t.Errorf("FormatMoney(%d) = %q, want %q", tt.kop, got, tt.want)
		}
	}
}

// Example-функция — тоже тест: go test сравнит вывод с комментарием.
func ExampleWriteReceipt() {
	WriteReceipt(os.Stdout, []Item{
		{Name: "Bread", Qty: 1, Price: 4500},
		{Name: "Milk", Qty: 2, Price: 8990},
	})
	// Output:
	// Bread     45.00
	// Milk ×2  179.80
	// Итого    224.80
}

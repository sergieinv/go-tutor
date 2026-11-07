package task

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// Item — позиция чека.
type Item struct {
	Name  string
	Qty   int
	Price int64 // цена за штуку в копейках
}

// ErrInvalidItem — позиция с пустым названием, Qty < 1 или Price < 0.
var ErrInvalidItem = errors.New("receipt: неверная позиция")

// FormatMoney печатает сумму в копейках как рубли с двумя знаками.
func FormatMoney(kop int64) string {
	sign := ""
	u := uint64(kop)
	if kop < 0 {
		sign = "-"
		u = -u // верно и для math.MinInt64
	}
	return fmt.Sprintf("%s%d.%02d", sign, u/100, u%100)
}

// WriteReceipt печатает чек в w.
func WriteReceipt(w io.Writer, items []Item) error {
	const totalLabel = "Итого"
	labels := make([]string, len(items))
	sums := make([]string, len(items))
	var total int64
	for i, it := range items {
		if it.Name == "" || it.Qty < 1 || it.Price < 0 {
			return fmt.Errorf("позиция %d: %w", i, ErrInvalidItem)
		}
		labels[i] = it.Name
		if it.Qty > 1 {
			labels[i] += " ×" + strconv.Itoa(it.Qty)
		}
		sum := int64(it.Qty) * it.Price
		sums[i] = FormatMoney(sum)
		total += sum
	}
	labels = append(labels, totalLabel)
	sums = append(sums, FormatMoney(total))

	// Ширина колонок: название — в рунах, сумма — ASCII, хватает len.
	nameW, sumW := 0, 0
	for i := range labels {
		nameW = max(nameW, utf8.RuneCountInString(labels[i]))
		sumW = max(sumW, len(sums[i]))
	}
	for i := range labels {
		// fmt считает ширину %-*s в рунах, а не в байтах.
		if _, err := fmt.Fprintf(w, "%-*s  %*s\n", nameW, labels[i], sumW, sums[i]); err != nil {
			return err
		}
	}
	return nil
}

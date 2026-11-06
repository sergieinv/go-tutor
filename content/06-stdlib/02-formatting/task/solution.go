package task

import (
	"fmt"
	"strings"
)

// Money — сумма в копейках.
type Money int64

// String форматирует сумму как "12.50", "-0.05", "0.00".
func (m Money) String() string {
	sign := ""
	if m < 0 {
		// Знак отдельно: у -5 копеек целая часть -5/100 == 0,
		// и минус потерялся бы.
		sign = "-"
		m = -m
	}
	return fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)
}

// Line — позиция чека.
type Line struct {
	Name  string
	Qty   int
	Price Money // цена за штуку
}

// Receipt печатает чек: строку на позицию и итоговую строку TOTAL.
func Receipt(lines []Line) string {
	var b strings.Builder
	var total Money
	for _, l := range lines {
		sum := l.Price * Money(l.Qty)
		total += sum
		// %v вызовет Money.String.
		fmt.Fprintf(&b, "%-12s%4d%12v\n", l.Name, l.Qty, sum)
	}
	fmt.Fprintf(&b, "%-16s%12v\n", "TOTAL", total)
	return b.String()
}

package task

// Money — сумма в копейках.
type Money int64

// String форматирует сумму как "12.50", "-0.05", "0.00".
func (m Money) String() string {
	// TODO: реализуйте метод
	return ""
}

// Line — позиция чека.
type Line struct {
	Name  string
	Qty   int
	Price Money // цена за штуку
}

// Receipt печатает чек: строку на позицию и итоговую строку TOTAL.
func Receipt(lines []Line) string {
	// TODO: реализуйте функцию
	return ""
}

package task

// Item — товар с ценой в копейках.
type Item struct {
	Name  string
	Price int
}

// FirstUnique возвращает первую руну, которая встречается в s ровно
// один раз, и её байтовый индекс. Если такой нет — 0, -1.
func FirstUnique(s string) (rune, int) {
	// TODO: реализуйте функцию
	return 0, 0
}

// ApplyDiscount уменьшает цену каждого товара на pct процентов
// прямо в items: Price = Price * (100 - pct) / 100.
func ApplyDiscount(items []Item, pct int) {
	// TODO: реализуйте функцию
}

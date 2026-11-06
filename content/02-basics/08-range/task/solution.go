package task

// Item — товар с ценой в копейках.
type Item struct {
	Name  string
	Price int
}

// FirstUnique возвращает первую руну, которая встречается в s ровно
// один раз, и её байтовый индекс. Если такой нет — 0, -1.
func FirstUnique(s string) (rune, int) {
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	// Второй проход в порядке строки: i — байтовое смещение руны.
	for i, r := range s {
		if counts[r] == 1 {
			return r, i
		}
	}
	return 0, -1
}

// ApplyDiscount уменьшает цену каждого товара на pct процентов
// прямо в items: Price = Price * (100 - pct) / 100.
func ApplyDiscount(items []Item, pct int) {
	// Через индекс: переменная v в range была бы копией.
	for i := range items {
		items[i].Price = items[i].Price * (100 - pct) / 100
	}
}

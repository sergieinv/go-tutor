// `for ... range` — это `foreach` в Go: он обходит
// срезы, массивы, строки, map, числа и каналы.

package main

import (
	"fmt"
	"maps"
	"slices"
)

type item struct {
	name string
	qty  int
}

func main() {

	// Для среза `range` выдаёт индекс и копию элемента.
	langs := []string{"go", "c#", "rust"}
	for i, l := range langs {
		fmt.Print(i, "=", l, " ")
	}
	fmt.Println()

	// Ненужное значение заменяют на `_`, а если нужен
	// только индекс, вторую переменную просто не пишут.
	total := 0
	for _, l := range langs {
		total += len(l)
	}
	for i := range langs {
		langs[i] = "<" + langs[i] + ">"
	}
	fmt.Println("total:", total, langs)

	// `v` — копия элемента. Изменение копии коллекцию
	// не меняет — меняют через индекс.
	items := []item{{"pen", 1}, {"cup", 2}}
	for _, it := range items {
		it.qty = 100
	}
	fmt.Println("via copy:", items)
	for i := range items {
		items[i].qty *= 10
	}
	fmt.Println("via index:", items)

	// Строку `range` декодирует как UTF-8: выдаёт
	// байтовый индекс начала руны и саму руну.
	for i, r := range "go-мир" {
		fmt.Printf("%d:%c ", i, r)
	}
	fmt.Println()

	// Порядок обхода map случаен. Если порядок
	// важен, обходят отсортированные ключи.
	stock := map[string]int{"pen": 3, "cup": 0, "ink": 7}
	for _, k := range slices.Sorted(maps.Keys(stock)) {
		fmt.Print(k, ":", stock[k], " ")
	}
	fmt.Println()

	// Range по целому числу (Go 1.22+): от 0 до n-1.
	// При n <= 0 тело не выполнится ни разу.
	for i := range 3 {
		fmt.Print(i, " ")
	}
	for range -1 {
		fmt.Print("никогда")
	}
	fmt.Println()

	// Выражение `range` вычисляется один раз: `append`
	// внутри цикла не добавляет итераций.
	nums := []int{1, 2, 3}
	for _, n := range nums {
		nums = append(nums, n*10)
	}
	fmt.Println("append:", nums)
}

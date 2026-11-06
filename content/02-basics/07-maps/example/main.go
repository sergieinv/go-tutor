// `map[K]V` — встроенная хеш-таблица. Ключом может
// быть любой сравнимый тип; срез, map и функция —
// нельзя.

package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

func main() {

	// `make` создаёт пустую map. Запись и чтение —
	// через индекс, `len` возвращает число пар.
	ages := make(map[string]int)
	ages["ann"] = 31
	ages["bob"] = 27
	fmt.Println("ann:", ages["ann"], "len:", len(ages))

	// Отсутствующий ключ — не ошибка: возвращается
	// нулевое значение типа.
	fmt.Println("eve:", ages["eve"])

	// Форма «comma ok» отличает «нет ключа» от
	// «ключ есть, значение нулевое». Это `TryGetValue`.
	ages["kid"] = 0
	_, okKid := ages["kid"]
	_, okEve := ages["eve"]
	fmt.Println("ok:", okKid, okEve)

	// `delete` удаляет ключ. Удалять отсутствующий
	// ключ можно — это ничего не делает.
	delete(ages, "kid")
	delete(ages, "nobody")
	fmt.Println("after delete:", len(ages))

	// Литерал map. `fmt.Println` печатает ключи
	// отсортированными — это свойство `fmt`, а не map.
	langs := map[string]int{"go": 2009, "c#": 2000, "rust": 2015}
	fmt.Println("literal:", langs)

	// Порядок `range` по map случаен и меняется от
	// запуска к запуску. Для стабильного порядка
	// сортируем ключи: `maps.Keys` + `slices.Sorted`.
	for _, k := range slices.Sorted(maps.Keys(langs)) {
		fmt.Print(k, "=", langs[k], " ")
	}
	fmt.Println()

	// Нулевое значение делает счётчики тривиальными:
	// `counts[w]++` работает и для нового ключа.
	counts := map[string]int{}
	for _, w := range strings.Fields("to be or not to be") {
		counts[w]++
	}
	fmt.Println("counts:", counts)

	// Переменная map ссылается на общую таблицу:
	// функция меняет ту же map, что и вызывающий код.
	addBonus(ages)
	fmt.Println("bonus:", ages)

	// `nil`-map можно читать и передавать в `len`,
	// но запись в неё паникует.
	var empty map[string]int
	fmt.Println("nil map:", empty == nil, len(empty), empty["x"])

	// `clear` удаляет все элементы, map остаётся
	// пригодной для записи.
	clear(ages)
	fmt.Println("clear:", len(ages), ages == nil)
}

func addBonus(m map[string]int) {
	for k := range m {
		m[k] += 10
	}
}

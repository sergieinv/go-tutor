// `sync.WaitGroup` — счётчик незавершённых задач.
// `Add` увеличивает его, `Done` уменьшает, `Wait`
// блокируется, пока счётчик не дойдёт до нуля. Это
// аналог `Task.WhenAll`, только без результатов:
// их собирают отдельно.

package main

import (
	"fmt"
	"strings"
	"sync"
)

func main() {
	words := []string{"go", "channel", "select", "mutex"}

	// Каждая горутина пишет в свой элемент среза — разные
	// элементы не пересекаются в памяти, поэтому гонки нет
	// и мьютекс не нужен. Порядок результатов совпадает
	// с порядком входа, как бы ни легли горутины.
	upper := make([]string, len(words))

	var wg sync.WaitGroup
	for i, w := range words {
		// `Add` — до `go`, в той горутине, что потом
		// вызовет `Wait`. Если перенести его внутрь новой
		// горутины, `Wait` может успеть увидеть ноль.
		wg.Add(1)
		go func() {
			// `defer` гарантирует `Done` при любом выходе,
			// в том числе раннем `return`.
			defer wg.Done()
			upper[i] = strings.ToUpper(w)
		}()
	}
	// После `Wait` все записи горутин видны: возврат
	// из `Done` «происходит до» возврата из `Wait`.
	wg.Wait()
	fmt.Println(upper)

	// С Go 1.25 то же пишется короче: `wg.Go` сам делает
	// `Add(1)`, запускает горутину и вызывает `Done`,
	// когда функция вернётся.
	lengths := make([]int, len(words))
	var wg2 sync.WaitGroup
	for i, w := range words {
		wg2.Go(func() {
			lengths[i] = len(w)
		})
	}
	wg2.Wait()
	fmt.Println(lengths)

	// WaitGroup передают только по указателю: копия
	// содержит свой счётчик, и `Done` на копии не дойдёт
	// до оригинала. `go vet` ловит такие копии (copylocks).
	var wg3 sync.WaitGroup
	squares := make([]int, 3)
	for i := range 3 {
		wg3.Add(1)
		go square(&wg3, squares, i)
	}
	wg3.Wait()
	fmt.Println(squares)
}

func square(wg *sync.WaitGroup, out []int, i int) {
	defer wg.Done()
	out[i] = i * i
}

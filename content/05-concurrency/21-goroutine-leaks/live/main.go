// «Сервер» с тремя утечками горутин. Запустите: итоговый шаг
// перечислит горутины, которые не завершились к выходу из main,
// и строки, где они ждут. Почините утечки по комментариям
// ИСПРАВЛЕНИЕ 1–3 и запустите снова — итог скажет «утечек нет».
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var errTimeout = errors.New("таймаут")

// fetch ждёт ответа медленного бэкенда не дольше timeout.
func fetch(id int, timeout time.Duration) (string, error) {
	ch := make(chan string) // ИСПРАВЛЕНИЕ 1: make(chan string, 1)
	go func() {
		time.Sleep(time.Duration(id) * 5 * time.Millisecond)
		ch <- fmt.Sprint("ответ ", id) // после таймаута здесь никто не ждёт
	}()
	select {
	case v := <-ch:
		return v, nil
	case <-time.After(timeout):
		return "", errTimeout
	}
}

// logger печатает строки, пока канал не закроют.
func logger(lines <-chan string) {
	for s := range lines {
		fmt.Println(s)
	}
}

// heartbeat тикает, пока жив сервер, — но условия выхода нет.
func heartbeat(done <-chan struct{}) {
	t := time.NewTicker(5 * time.Millisecond) // ИСПРАВЛЕНИЕ 3: defer t.Stop()
	for {
		select {
		case <-t.C:
			// ИСПРАВЛЕНИЕ 3: case <-done: return
		}
	}
}

func main() {
	done := make(chan struct{})
	go heartbeat(done)

	lines := make(chan string)
	go logger(lines)

	var wg sync.WaitGroup
	for id := 1; id <= 5; id++ {
		wg.Add(1)
		go func() { // обработчик запроса
			defer wg.Done()
			v, err := fetch(id, 12*time.Millisecond)
			if err != nil {
				v = err.Error()
			}
			lines <- fmt.Sprintf("запрос %d: %s", id, v)
		}()
	}
	wg.Wait()
	// ИСПРАВЛЕНИЕ 2: close(lines) — иначе logger ждёт вечно.
	// ИСПРАВЛЕНИЕ 3: close(done) — иначе heartbeat тикает вечно.

	// Сервер работает дальше: даём горутинам время завершиться.
	time.Sleep(40 * time.Millisecond)
	fmt.Println("обработано запросов:", 5)
}

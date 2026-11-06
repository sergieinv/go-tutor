// `sync.Once` выполняет функцию ровно один раз, сколько
// бы горутин ни пришло одновременно. Остальные ждут, пока
// первая закончит, — ленивая инициализация без гонок.

package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

type config struct{ dsn string }

var (
	cfg     *config
	cfgOnce sync.Once
	loads   atomic.Int32
)

// Классический приём: глобальная переменная и `Once`
// рядом с ней. `Do` возвращается только после того, как
// функция инициализации завершилась, — `cfg` уже готов.
func getConfig() *config {
	cfgOnce.Do(func() {
		loads.Add(1)
		cfg = &config{dsn: "postgres://localhost/app"}
	})
	return cfg
}

// С Go 1.21 то же самое короче: `sync.OnceValue` оборачивает
// функцию со значением и сам хранит результат.
var hostname = sync.OnceValue(func() string {
	fmt.Println("вычисляем hostname")
	return strings.ToLower("BUILD-01")
})

// `OnceValues` — для пары «значение, ошибка». Ошибка
// тоже запоминается: повторного вызова не будет.
var token = sync.OnceValues(func() (string, error) {
	fmt.Println("читаем токен")
	return "", errors.New("нет файла с токеном")
})

func main() {
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { getConfig() })
	}
	wg.Wait()
	fmt.Println(getConfig().dsn, "— загрузок:", loads.Load())

	fmt.Println(hostname(), hostname())

	for range 2 {
		_, err := token()
		fmt.Println("ошибка:", err)
	}

	// Если функция в `Do` паникует, `Once` считает её
	// выполненной: второй `Do` уже ничего не вызовет.
	var once sync.Once
	call := func() {
		defer func() { fmt.Println("recover:", recover()) }()
		once.Do(func() { panic("сбой инициализации") })
	}
	call()
	call()

	// А `OnceFunc` (и `OnceValue`) запоминают панику
	// и повторяют её при каждом вызове.
	setup := sync.OnceFunc(func() { panic("сбой инициализации") })
	for range 2 {
		func() {
			defer func() { fmt.Println("recover:", recover()) }()
			setup()
		}()
	}
}

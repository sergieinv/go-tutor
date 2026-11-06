// Таймаут в Go — это `select` между результатом работы
// и таймером. Саму работу таймаут не прерывает: убить
// горутину снаружи нельзя. Поэтому исполнитель нужно
// устроить так, чтобы после таймаута он мог спокойно
// доработать и завершиться.

package main

import (
	"errors"
	"fmt"
	"runtime"
	"time"
)

var ErrTimeout = errors.New("таймаут")

// Значение и ошибку удобно передавать по каналу
// одной структурой.
type result struct {
	value string
	err   error
}

// `fetch` запускает `work` в отдельной горутине и ждёт
// её не дольше `timeout`.
func fetch(work func() (string, error), timeout time.Duration) (string, error) {
	// Буфер на одно значение — ключевая деталь. Если мы
	// уйдём по таймауту, исполнитель всё равно положит
	// результат в буфер и завершится. Читать его никто
	// не будет — канал просто соберёт сборщик мусора.
	ch := make(chan result, 1)
	go func() {
		v, err := work()
		ch <- result{v, err}
	}()

	select {
	case r := <-ch:
		return r.value, r.err
	case <-time.After(timeout):
		return "", ErrTimeout
	}
}

// Та же функция с небуферизованным каналом: после
// таймаута исполнитель навсегда застрянет на отправке.
func fetchLeaky(work func() (string, error), timeout time.Duration) (string, error) {
	ch := make(chan result)
	go func() {
		v, err := work()
		ch <- result{v, err} // некому принять — утечка
	}()

	select {
	case r := <-ch:
		return r.value, r.err
	case <-time.After(timeout):
		return "", ErrTimeout
	}
}

// Ждём, пока число горутин опустится до `want`,
// но не дольше секунды.
func goroutinesAfterSettle(want int) int {
	for range 100 {
		if n := runtime.NumGoroutine(); n <= want {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}

// Медленная работа: возвращает результат, только когда
// закроют `release`.
func slowWork(release <-chan struct{}) func() (string, error) {
	return func() (string, error) {
		<-release
		return "поздно", nil
	}
}

func main() {
	// Быстрая операция укладывается в лимит, её
	// значение и ошибка проходят насквозь.
	v, err := fetch(func() (string, error) { return "данные", nil }, time.Second)
	fmt.Println(v, err)
	_, err = fetch(func() (string, error) { return "", errors.New("нет связи") }, time.Second)
	fmt.Println("ошибка:", err)

	// Медленная операция ждёт сигнала `release`, которого
	// до таймаута не будет.
	release := make(chan struct{})
	_, err = fetch(slowWork(release), 50*time.Millisecond)
	fmt.Println("ошибка:", err, errors.Is(err, ErrTimeout))

	// Вызывающий уже ушёл, а исполнитель ещё работает.
	// Отпускаем его: результат ляжет в буфер, горутина
	// завершится, и останется только `main`.
	close(release)
	fmt.Println("горутин после fetch:", goroutinesAfterSettle(1))

	// С небуферизованным каналом исполнитель доработал,
	// но висит на отправке — и будет висеть до конца
	// программы.
	release = make(chan struct{})
	_, err = fetchLeaky(slowWork(release), 50*time.Millisecond)
	fmt.Println("ошибка:", err)
	close(release)
	fmt.Println("горутин после fetchLeaky:", goroutinesAfterSettle(1))
}

// Обычные отправка и получение блокируют горутину, пока
// канал не готов. `select` с веткой `default` превращает
// их в попытку: получилось — хорошо, нет — идём дальше.

package main

import "fmt"

// Попытка прочитать: второе значение сообщает, удалось ли.
// Это аналог `TryRead` у `System.Threading.Channels`.
func tryRecv(ch <-chan string) (string, bool) {
	select {
	case v := <-ch:
		return v, true
	default:
		return "", false
	}
}

// Логгер, который никогда не тормозит вызывающего: если
// буфер полон, сообщение отбрасывается и учитывается.
// Здесь `Log` зовут из одной горутины; если писателей
// много, счётчик нужен атомарный — `atomic.Int64`.
type Logger struct {
	queue   chan string
	dropped int
}

func (l *Logger) Log(msg string) {
	select {
	case l.queue <- msg:
	default:
		l.dropped++ // писатель не успевает — теряем, но не ждём
	}
}

func main() {
	ch := make(chan string, 1)

	// Канал пуст — сразу срабатывает `default`.
	_, ok := tryRecv(ch)
	fmt.Println("есть сообщение:", ok)

	ch <- "привет"
	v, ok := tryRecv(ch)
	fmt.Println("есть сообщение:", ok, v)

	// Буфер на 3 сообщения, а читателя нет вовсе:
	// первые три лягут в очередь, остальные отбросятся.
	logger := &Logger{queue: make(chan string, 3)}
	for i := range 5 {
		logger.Log(fmt.Sprint("событие ", i))
	}
	fmt.Println("в очереди:", len(logger.queue), "потеряно:", logger.dropped)

	// Забрать всё, что накопилось, и не ждать новых:
	// цикл с `default` заканчивается на пустом канале.
	var batch []string
drain:
	for {
		select {
		case m := <-logger.queue:
			batch = append(batch, m)
		default:
			break drain
		}
	}
	fmt.Println("пачка:", batch)

	// Канал с буфером 1 — простейший семафор. Неблокирующая
	// отправка работает как try-lock: заняли — работаем,
	// занято — делаем что-то другое.
	sem := make(chan struct{}, 1)
	tryLock := func() bool {
		select {
		case sem <- struct{}{}:
			return true
		default:
			return false
		}
	}
	fmt.Println("первая попытка:", tryLock())
	fmt.Println("вторая попытка:", tryLock())
	<-sem // освобождаем
	fmt.Println("после освобождения:", tryLock())
}

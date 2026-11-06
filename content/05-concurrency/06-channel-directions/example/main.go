// Тип канала может разрешать только одно направление:
// `chan<- T` — только отправка, `<-chan T` — только
// получение. В сигнатурах функций это документирует
// роли и превращает ошибку в ошибку компиляции.

package main

import "fmt"

// Генератор возвращает `<-chan int`: вызывающий может
// только читать. Закрыть такой канал или отправить в него
// снаружи не получится — владелец канала один.
func count(from, to int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := from; i < to; i++ {
			out <- i
		}
	}()
	return out // chan int неявно становится <-chan int
}

// Стадия обработки: читает из `in`, пишет в свой `out`.
// По сигнатуре видно, откуда данные приходят и куда уходят.
func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			out <- v * v
		}
	}()
	return out
}

// Потребитель получает `chan<- string`: только отправка.
// Прочитать из `log` внутри функции компилятор не даст.
func report(sum int, log chan<- string) {
	log <- fmt.Sprint("сумма квадратов: ", sum)
}

func main() {

	// Генераторы собираются в цепочку: каждая стадия
	// работает в своей горутине.
	sum := 0
	for v := range square(count(1, 5)) {
		fmt.Println("квадрат:", v)
		sum += v
	}

	// Двунаправленный канал неявно преобразуется
	// в однонаправленный при передаче в функцию.
	// Обратного преобразования нет.
	log := make(chan string, 1)
	report(sum, log)
	fmt.Println(<-log)

	// Однонаправленную переменную можно объявить и явно.
	// Через `recv` можно только читать, через `send` —
	// только писать, хотя канал под ними один.
	ch := make(chan int, 1)
	var send chan<- int = ch
	var recv <-chan int = ch
	send <- 42
	fmt.Println("через recv:", <-recv)
	fmt.Printf("типы: %T, %T, %T\n", ch, send, recv)
}

// `time.Timer` — одно событие в будущем, `time.Ticker` —
// событие каждые `d`. Оба доставляют время через канал
// `C`, поэтому их ждут обычным получением или в `select`.

package main

import (
	"fmt"
	"time"
)

func main() {
	start := time.Now()

	// Таймер срабатывает один раз: через `d` в `C` придёт
	// время срабатывания.
	t := time.NewTimer(50 * time.Millisecond)
	<-t.C
	fmt.Println("таймер сработал, прошло ≥ 50мс:", time.Since(start) >= 50*time.Millisecond)

	// `Stop` отменяет таймер, который ещё не сработал,
	// и возвращает `true`. С Go 1.23 после `Stop` в `C`
	// гарантированно не окажется устаревшего значения —
	// вычитывать канал «на всякий случай» не нужно.
	t2 := time.NewTimer(time.Hour)
	fmt.Println("остановлен до срабатывания:", t2.Stop())
	fmt.Println("повторный Stop:", t2.Stop())

	// `Reset` переводит таймер на новый срок — и для
	// остановленного, и для активного таймера.
	t2.Reset(20 * time.Millisecond)
	<-t2.C
	fmt.Println("после Reset сработал")

	// `AfterFunc` вызывает функцию в отдельной горутине.
	// Канал `C` у такого таймера не используется (он `nil`),
	// поэтому о срабатывании сообщаем сами.
	done := make(chan string)
	time.AfterFunc(10*time.Millisecond, func() {
		done <- "AfterFunc выполнен"
	})
	fmt.Println(<-done)

	// Тикер присылает время каждые `d`, пока его
	// не остановят. `Stop` не закрывает канал, поэтому
	// `for range ticker.C` после `Stop` просто зависнет.
	ticker := time.NewTicker(10 * time.Millisecond)
	for i := range 3 {
		<-ticker.C
		fmt.Println("тик", i+1)
	}

	// Если читатель не успевает, тики не копятся:
	// пропущенные теряются, а не ждут в очереди. Читаем
	// раз в 50 мс при периоде 10 мс — между полученными
	// тиками около 50 мс, а не 10.
	var prev time.Time
	for i := range 4 {
		time.Sleep(50 * time.Millisecond)
		tick := <-ticker.C
		if i > 0 {
			fmt.Println("между тиками ≥ 40мс:", tick.Sub(prev) >= 40*time.Millisecond)
		}
		prev = tick
	}
	ticker.Stop()
}

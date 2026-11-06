package task

// Alternate вызывает a(0), b(0), a(1), b(1), …, a(n-1), b(n-1):
// все вызовы a — в одной горутине, все вызовы b — в другой,
// очерёдность передаётся через каналы.
func Alternate(n int, a, b func(i int)) {
	if n <= 0 {
		return // иначе первый сигнал некому было бы получить
	}
	aTurn := make(chan struct{})
	bTurn := make(chan struct{})
	done := make(chan struct{})

	go func() {
		for i := range n {
			<-aTurn // ждём своего хода
			a(i)
			bTurn <- struct{}{} // передаём ход
		}
	}()
	go func() {
		for i := range n {
			<-bTurn
			b(i)
			if i < n-1 {
				aTurn <- struct{}{}
			}
		}
		done <- struct{}{} // последний ход сделан
	}()

	aTurn <- struct{}{} // первый ход за a
	<-done
}

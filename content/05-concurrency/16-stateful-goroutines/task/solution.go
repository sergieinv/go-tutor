package task

import "errors"

// ErrInsufficientFunds — на счёте не хватает денег.
var ErrInsufficientFunds = errors.New("insufficient funds")

// Bank хранит балансы счетов. Состоянием владеет одна горутина.
type Bank struct {
	ops  chan func(map[string]int)
	done chan struct{}
}

// NewBank запускает горутину-владельца.
func NewBank() *Bank {
	b := &Bank{ops: make(chan func(map[string]int)), done: make(chan struct{})}
	go func() {
		defer close(b.done)
		balances := map[string]int{}
		// Операции выполняются строго по одной: перевод
		// не может перемешаться с другим запросом.
		for op := range b.ops {
			op(balances)
		}
	}()
	return b
}

// do отправляет операцию владельцу и ждёт результат.
func (b *Bank) do(op func(map[string]int) error) error {
	resp := make(chan error, 1)
	b.ops <- func(m map[string]int) { resp <- op(m) }
	return <-resp
}

// Deposit зачисляет amount на счёт.
func (b *Bank) Deposit(account string, amount int) {
	b.do(func(m map[string]int) error {
		m[account] += amount
		return nil
	})
}

// Withdraw списывает amount или возвращает ErrInsufficientFunds.
func (b *Bank) Withdraw(account string, amount int) error {
	return b.do(func(m map[string]int) error {
		if m[account] < amount {
			return ErrInsufficientFunds
		}
		m[account] -= amount
		return nil
	})
}

// Transfer атомарно переводит amount со счёта from на счёт to.
func (b *Bank) Transfer(from, to string, amount int) error {
	return b.do(func(m map[string]int) error {
		if m[from] < amount {
			return ErrInsufficientFunds
		}
		m[from] -= amount
		m[to] += amount
		return nil
	})
}

// Balance возвращает баланс счёта.
func (b *Bank) Balance(account string) int {
	var v int
	b.do(func(m map[string]int) error {
		v = m[account]
		return nil
	})
	return v
}

// Close останавливает горутину-владельца и дожидается её завершения.
func (b *Bank) Close() {
	close(b.ops)
	<-b.done
}

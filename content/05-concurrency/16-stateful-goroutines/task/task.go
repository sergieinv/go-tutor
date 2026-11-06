package task

import "errors"

// ErrInsufficientFunds — на счёте не хватает денег.
var ErrInsufficientFunds = errors.New("insufficient funds")

// Bank хранит балансы счетов. Состоянием владеет одна горутина.
type Bank struct {
	// TODO: каналы для общения с горутиной-владельцем
}

// NewBank запускает горутину-владельца.
func NewBank() *Bank {
	// TODO: реализуйте функцию
	return &Bank{}
}

// Deposit зачисляет amount на счёт.
func (b *Bank) Deposit(account string, amount int) {
	// TODO
}

// Withdraw списывает amount или возвращает ErrInsufficientFunds.
func (b *Bank) Withdraw(account string, amount int) error {
	// TODO
	return nil
}

// Transfer атомарно переводит amount со счёта from на счёт to.
func (b *Bank) Transfer(from, to string, amount int) error {
	// TODO
	return nil
}

// Balance возвращает баланс счёта.
func (b *Bank) Balance(account string) int {
	// TODO
	return 0
}

// Close останавливает горутину-владельца и дожидается её завершения.
func (b *Bank) Close() {
	// TODO
}

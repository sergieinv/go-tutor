package task

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidAmount     = errors.New("invalid amount")
	ErrNoAccount         = errors.New("no such account")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Withdraw списывает amount со счёта account.
// Ошибки: ErrInvalidAmount, ErrNoAccount, ErrInsufficientFunds.
// При ошибке баланс не меняется.
func Withdraw(balances map[string]int, account string, amount int) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	// Чтение из nil-map безопасно: ok будет false.
	balance, ok := balances[account]
	if !ok {
		// Контекст в тексте не мешает errors.Is: %w сохраняет sentinel.
		return fmt.Errorf("account %q: %w", account, ErrNoAccount)
	}
	if balance < amount {
		return fmt.Errorf("account %q: balance %d, want %d: %w", account, balance, amount, ErrInsufficientFunds)
	}
	balances[account] = balance - amount
	return nil
}

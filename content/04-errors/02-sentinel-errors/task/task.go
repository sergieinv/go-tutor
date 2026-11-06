package task

import "errors"

var (
	ErrInvalidAmount     = errors.New("invalid amount")
	ErrNoAccount         = errors.New("no such account")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Withdraw списывает amount со счёта account.
// Ошибки: ErrInvalidAmount, ErrNoAccount, ErrInsufficientFunds.
// При ошибке баланс не меняется.
func Withdraw(balances map[string]int, account string, amount int) error {
	// TODO: реализуйте функцию
	return nil
}

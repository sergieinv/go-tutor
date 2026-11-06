package task

import "errors"

var (
	ErrInvalidAmount     = errors.New("invalid amount")
	ErrUnknownAccount    = errors.New("unknown account")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Bank хранит балансы счетов и безопасен для одновременного
// использования из многих горутин.
type Bank struct {
	// TODO: поля
}

// NewBank создаёт банк с копией переданных балансов.
func NewBank(balances map[string]int) *Bank {
	// TODO: реализуйте функцию
	return &Bank{}
}

// Balance возвращает баланс счёта; false — если счёта нет.
func (b *Bank) Balance(name string) (int, bool) {
	// TODO: реализуйте метод
	return 0, false
}

// Transfer переводит amount со счёта from на счёт to.
func (b *Bank) Transfer(from, to string, amount int) error {
	// TODO: реализуйте метод
	return nil
}

// Total возвращает сумму балансов всех счетов.
func (b *Bank) Total() int {
	// TODO: реализуйте метод
	return 0
}

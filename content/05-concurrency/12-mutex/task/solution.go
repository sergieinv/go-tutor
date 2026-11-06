package task

import (
	"errors"
	"maps"
	"sync"
)

var (
	ErrInvalidAmount     = errors.New("invalid amount")
	ErrUnknownAccount    = errors.New("unknown account")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Bank хранит балансы счетов и безопасен для одновременного
// использования из многих горутин.
type Bank struct {
	// Один мьютекс на все счета: инвариант «сумма постоянна»
	// охватывает несколько полей, и Total должен видеть их
	// согласованными.
	mu       sync.Mutex
	balances map[string]int
}

// NewBank создаёт банк с копией переданных балансов.
func NewBank(balances map[string]int) *Bank {
	m := maps.Clone(balances)
	if m == nil {
		m = map[string]int{}
	}
	return &Bank{balances: m}
}

// Balance возвращает баланс счёта; false — если счёта нет.
func (b *Bank) Balance(name string) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.balances[name]
	return v, ok
}

// Transfer переводит amount со счёта from на счёт to.
func (b *Bank) Transfer(from, to string, amount int) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	fromBal, ok1 := b.balances[from]
	_, ok2 := b.balances[to]
	if !ok1 || !ok2 {
		return ErrUnknownAccount
	}
	if fromBal < amount {
		return ErrInsufficientFunds
	}
	// Проверка и списание — под одним замком: между ними
	// никто не успеет потратить те же деньги.
	b.balances[from] -= amount
	b.balances[to] += amount
	return nil
}

// Total возвращает сумму балансов всех счетов.
func (b *Bank) Total() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	sum := 0
	for _, v := range b.balances {
		sum += v
	}
	return sum
}

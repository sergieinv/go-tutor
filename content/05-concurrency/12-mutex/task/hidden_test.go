package task

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewBankCopiesMap(t *testing.T) {
	src := map[string]int{"alice": 10}
	b := NewBank(src)
	src["alice"] = 1000
	src["bob"] = 5
	if v, _ := b.Balance("alice"); v != 10 {
		t.Errorf("alice = %d, want 10: NewBank должен копировать балансы", v)
	}
	if _, ok := b.Balance("bob"); ok {
		t.Error("счёт bob появился в банке после изменения исходной map")
	}
	b.Transfer("alice", "alice", 1)
	if src["alice"] != 1000 {
		t.Error("банк изменил исходную map")
	}
}

func TestNewBankNil(t *testing.T) {
	b := NewBank(nil)
	if got := b.Total(); got != 0 {
		t.Errorf("Total() = %d, want 0", got)
	}
	if err := b.Transfer("a", "b", 1); !errors.Is(err, ErrUnknownAccount) {
		t.Errorf("Transfer в пустом банке = %v, want ErrUnknownAccount", err)
	}
}

func TestTransferErrorOrder(t *testing.T) {
	b := NewBank(map[string]int{"alice": 5})
	if err := b.Transfer("nobody", "alice", 0); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("сумма проверяется первой: got %v, want ErrInvalidAmount", err)
	}
	if err := b.Transfer("alice", "nobody", 100); !errors.Is(err, ErrUnknownAccount) {
		t.Errorf("счета проверяются раньше остатка: got %v, want ErrUnknownAccount", err)
	}
}

func TestTransferWholeBalanceAndSelf(t *testing.T) {
	b := NewBank(map[string]int{"alice": 40, "bob": 0})
	if err := b.Transfer("alice", "alice", 40); err != nil {
		t.Fatalf("перевод самому себе вернул %v", err)
	}
	if v, _ := b.Balance("alice"); v != 40 {
		t.Fatalf("после перевода самому себе alice = %d, want 40", v)
	}
	if err := b.Transfer("alice", "alice", 41); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("перевод самому себе больше остатка = %v, want ErrInsufficientFunds", err)
	}
	if err := b.Transfer("alice", "bob", 40); err != nil {
		t.Fatalf("перевод всего остатка вернул %v", err)
	}
	if v, _ := b.Balance("alice"); v != 0 {
		t.Fatalf("alice = %d, want 0", v)
	}
}

func TestBankConcurrent(t *testing.T) {
	const accounts, start = 8, 1000
	init := map[string]int{}
	for i := range accounts {
		init[fmt.Sprint("acc", i)] = start
	}
	b := NewBank(init)
	want := accounts * start

	var wg sync.WaitGroup
	stop := make(chan struct{})
	bad := make(chan int, 1)

	// Наблюдатель: сумма в любой момент должна быть постоянной.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if got := b.Total(); got != want {
				select {
				case bad <- got:
				default:
				}
				return
			}
		}
	}()

	var workers sync.WaitGroup
	for w := range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range 2000 {
				from := fmt.Sprint("acc", (w+i)%accounts)
				to := fmt.Sprint("acc", (w+i*3+1)%accounts)
				err := b.Transfer(from, to, 1+(i%300))
				if err != nil && !errors.Is(err, ErrInsufficientFunds) {
					t.Errorf("Transfer(%s, %s) = %v", from, to, err)
					return
				}
				if v, ok := b.Balance(from); !ok || v < 0 {
					t.Errorf("баланс %s = %d, ok=%v", from, v, ok)
					return
				}
			}
		}()
	}
	workers.Wait()
	close(stop)
	wg.Wait()

	select {
	case got := <-bad:
		t.Fatalf("Total() во время переводов вернул %d, want %d: сумма видна несогласованной", got, want)
	default:
	}
	if got := b.Total(); got != want {
		t.Fatalf("Total() = %d, want %d", got, want)
	}
}

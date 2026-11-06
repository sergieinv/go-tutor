package task

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func TestBankConcurrentDeposits(t *testing.T) {
	b := NewBank()
	defer b.Close()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 20 {
				b.Deposit("acc", 1)
			}
		})
	}
	wg.Wait()
	if got := b.Balance("acc"); got != 1000 {
		t.Fatalf("Balance = %d, want 1000: потеряны зачисления", got)
	}
}

func TestBankConcurrentWithdrawNoOverdraft(t *testing.T) {
	b := NewBank()
	defer b.Close()
	b.Deposit("acc", 50)

	var ok, failed atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			err := b.Withdraw("acc", 1)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrInsufficientFunds):
				failed.Add(1)
			default:
				t.Errorf("Withdraw вернул неожиданную ошибку %v", err)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 50 || failed.Load() != 50 || b.Balance("acc") != 0 {
		t.Fatalf("успешных списаний %d, отказов %d, баланс %d; want 50, 50, 0",
			ok.Load(), failed.Load(), b.Balance("acc"))
	}
}

func TestBankTransfersKeepTotal(t *testing.T) {
	b := NewBank()
	defer b.Close()
	accounts := []string{"a", "b", "c", "d"}
	for _, acc := range accounts {
		b.Deposit(acc, 100)
	}

	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			from, to := accounts[i%4], accounts[(i+1)%4]
			for range 50 {
				if err := b.Transfer(from, to, 7); err != nil && !errors.Is(err, ErrInsufficientFunds) {
					t.Errorf("Transfer вернул неожиданную ошибку %v", err)
				}
			}
		})
	}
	wg.Wait()

	total := 0
	for _, acc := range accounts {
		bal := b.Balance(acc)
		if bal < 0 {
			t.Errorf("баланс %s = %d: ушли в минус", acc, bal)
		}
		total += bal
	}
	if total != 400 {
		t.Fatalf("сумма балансов = %d, want 400: перевод не атомарен", total)
	}
}

func TestBankTransferToSelf(t *testing.T) {
	b := NewBank()
	defer b.Close()
	b.Deposit("a", 10)
	if err := b.Transfer("a", "a", 10); err != nil {
		t.Fatalf("Transfer(a, a, 10) = %v, want nil", err)
	}
	if got := b.Balance("a"); got != 10 {
		t.Fatalf("после перевода самому себе Balance = %d, want 10", got)
	}
	if err := b.Transfer("nobody", "a", 1); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Transfer с несуществующего счёта = %v, want ErrInsufficientFunds", err)
	}
}

func TestBankOwnerGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	b := NewBank()
	b.Deposit("a", 1)
	if runtime.NumGoroutine() <= before {
		t.Error("NewBank не запустил горутину-владельца")
	}
	b.Close()
	if !waitFor(time.Second, func() bool { return runtime.NumGoroutine() <= before }) {
		t.Errorf("горутин до NewBank: %d, после Close: %d — владелец не завершился", before, runtime.NumGoroutine())
	}
}

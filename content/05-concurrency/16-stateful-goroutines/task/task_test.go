package task

import (
	"errors"
	"testing"
)

func TestBankBasics(t *testing.T) {
	b := NewBank()
	defer b.Close()

	if got := b.Balance("alice"); got != 0 {
		t.Fatalf("Balance нового счёта = %d, want 0", got)
	}
	b.Deposit("alice", 100)
	if got := b.Balance("alice"); got != 100 {
		t.Fatalf("после Deposit(100) Balance = %d, want 100", got)
	}
	if err := b.Withdraw("alice", 30); err != nil {
		t.Fatalf("Withdraw(30) = %v, want nil", err)
	}
	if err := b.Withdraw("alice", 100); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Withdraw(100) при балансе 70 = %v, want ErrInsufficientFunds", err)
	}
	if got := b.Balance("alice"); got != 70 {
		t.Fatalf("Balance = %d, want 70: неудачный Withdraw не должен менять баланс", got)
	}
}

func TestBankTransfer(t *testing.T) {
	b := NewBank()
	defer b.Close()

	b.Deposit("alice", 50)
	if err := b.Transfer("alice", "bob", 20); err != nil {
		t.Fatalf("Transfer(20) = %v, want nil", err)
	}
	if err := b.Transfer("alice", "bob", 31); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Transfer(31) при балансе 30 = %v, want ErrInsufficientFunds", err)
	}
	if a, bb := b.Balance("alice"), b.Balance("bob"); a != 30 || bb != 20 {
		t.Fatalf("балансы alice=%d bob=%d, want 30 и 20", a, bb)
	}
}

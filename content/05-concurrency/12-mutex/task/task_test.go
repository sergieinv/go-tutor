package task

import (
	"errors"
	"testing"
)

func TestTransfer(t *testing.T) {
	b := NewBank(map[string]int{"alice": 100, "bob": 50})
	if err := b.Transfer("alice", "bob", 30); err != nil {
		t.Fatalf("Transfer вернул %v", err)
	}
	if v, _ := b.Balance("alice"); v != 70 {
		t.Errorf("alice = %d, want 70", v)
	}
	if v, _ := b.Balance("bob"); v != 80 {
		t.Errorf("bob = %d, want 80", v)
	}
	if got := b.Total(); got != 150 {
		t.Errorf("Total() = %d, want 150", got)
	}
}

func TestTransferErrors(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		amount   int
		want     error
	}{
		{"ноль", "alice", "bob", 0, ErrInvalidAmount},
		{"отрицательная сумма", "alice", "bob", -5, ErrInvalidAmount},
		{"нет отправителя", "carol", "bob", 10, ErrUnknownAccount},
		{"нет получателя", "alice", "carol", 10, ErrUnknownAccount},
		{"не хватает денег", "bob", "alice", 51, ErrInsufficientFunds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBank(map[string]int{"alice": 100, "bob": 50})
			if err := b.Transfer(tt.from, tt.to, tt.amount); !errors.Is(err, tt.want) {
				t.Fatalf("Transfer(%q, %q, %d) = %v, want %v", tt.from, tt.to, tt.amount, err, tt.want)
			}
			a, _ := b.Balance("alice")
			c, _ := b.Balance("bob")
			if a != 100 || c != 50 {
				t.Errorf("после ошибки балансы изменились: alice=%d bob=%d", a, c)
			}
		})
	}
}

func TestBalanceUnknown(t *testing.T) {
	b := NewBank(map[string]int{"alice": 1})
	if v, ok := b.Balance("bob"); ok || v != 0 {
		t.Fatalf("Balance(bob) = (%d, %v), want (0, false)", v, ok)
	}
}

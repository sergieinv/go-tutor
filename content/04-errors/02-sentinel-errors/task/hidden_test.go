package task

import (
	"errors"
	"testing"
)

func TestWithdrawEdges(t *testing.T) {
	tests := []struct {
		name     string
		balances map[string]int
		account  string
		amount   int
		want     error
	}{
		{"exact balance", map[string]int{"a": 50}, "a", 50, nil},
		{"negative amount", map[string]int{"a": 50}, "a", -5, ErrInvalidAmount},
		{"nil map", nil, "a", 10, ErrNoAccount},
		{"amount checked first", nil, "a", 0, ErrInvalidAmount},
		{"account before balance", map[string]int{"a": 0}, "b", 1000, ErrNoAccount},
		{"zero balance", map[string]int{"a": 0}, "a", 1, ErrInsufficientFunds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Withdraw(tt.balances, tt.account, tt.amount)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Withdraw вернул %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Withdraw вернул %v, want errors.Is(err, %v)", err, tt.want)
			}
		})
	}
}

func TestWithdrawExactLeavesZero(t *testing.T) {
	b := map[string]int{"a": 50}
	if err := Withdraw(b, "a", 50); err != nil {
		t.Fatalf("Withdraw вернул %v", err)
	}
	if v, ok := b["a"]; !ok || v != 0 {
		t.Errorf("после списания всего баланса b[a] = (%d, %v), want (0, true)", v, ok)
	}
}

func TestWithdrawDoesNotCreateAccount(t *testing.T) {
	b := map[string]int{"a": 50}
	_ = Withdraw(b, "ghost", 10)
	if _, ok := b["ghost"]; ok {
		t.Errorf("Withdraw создал счёт ghost при ошибке: %v", b)
	}
}

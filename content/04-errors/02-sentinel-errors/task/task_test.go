package task

import (
	"errors"
	"testing"
)

func TestWithdraw(t *testing.T) {
	tests := []struct {
		name    string
		account string
		amount  int
		want    error
		balance int // баланс alice после вызова
	}{
		{"ok", "alice", 30, nil, 70},
		{"no account", "bob", 10, ErrNoAccount, 100},
		{"insufficient", "alice", 101, ErrInsufficientFunds, 100},
		{"zero amount", "alice", 0, ErrInvalidAmount, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := map[string]int{"alice": 100}
			err := Withdraw(b, tt.account, tt.amount)
			if tt.want == nil && err != nil {
				t.Fatalf("Withdraw вернул ошибку %v, want nil", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("Withdraw вернул %v, want errors.Is(err, %v)", err, tt.want)
			}
			if b["alice"] != tt.balance {
				t.Errorf("баланс alice = %d, want %d", b["alice"], tt.balance)
			}
		})
	}
}

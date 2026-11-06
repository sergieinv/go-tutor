package task

import (
	"errors"
	"testing"
)

func TestSafeCallNormal(t *testing.T) {
	got, err := SafeCall(func() int { return 42 })
	if got != 42 || err != nil {
		t.Fatalf("SafeCall = (%d, %v), want (42, nil)", got, err)
	}
}

func TestSafeCallPanics(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name  string
		value any
		text  string
	}{
		{"string", "oops", "panic: oops"},
		{"error", boom, "panic: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeCall(func() int { panic(tt.value) })
			if err == nil {
				t.Fatalf("SafeCall вернул nil-ошибку при панике")
			}
			if got != 0 || err.Error() != tt.text {
				t.Errorf("SafeCall = (%d, %q), want (0, %q)", got, err, tt.text)
			}
		})
	}
}

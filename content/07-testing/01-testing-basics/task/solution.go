package task

import (
	"errors"
	"os"
	"testing"
)

// RequireEnv возвращает значение переменной окружения key
// или пропускает тест, если она не задана.
func RequireEnv(tb testing.TB, key string) string {
	v := os.Getenv(key)
	if v == "" {
		tb.Skipf("%s не задана", key)
	}
	return v
}

// CheckDiscount проверяет, что discount(price, percent) == want без ошибки.
func CheckDiscount(tb testing.TB, discount func(int, int) (int, error), price, percent, want int) {
	got, err := discount(price, percent)
	if err != nil {
		// Без результата сравнивать нечего — останавливаем тест.
		tb.Fatalf("Discount(%d, %d): неожиданная ошибка: %v", price, percent, err)
	}
	if got != want {
		tb.Errorf("Discount(%d, %d) = %d, want %d", price, percent, got, want)
	}
}

// CheckDiscountError проверяет, что discount(price, percent) вернула ошибку target.
func CheckDiscountError(tb testing.TB, discount func(int, int) (int, error), price, percent int, target error) {
	got, err := discount(price, percent)
	switch {
	case err == nil:
		tb.Errorf("Discount(%d, %d) = %d, nil; want error %v", price, percent, got, target)
	case !errors.Is(err, target):
		tb.Errorf("Discount(%d, %d): err = %v, want %v", price, percent, err, target)
	}
}

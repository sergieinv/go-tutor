package task

import "testing"

// RequireEnv возвращает значение переменной окружения key
// или пропускает тест, если она не задана.
func RequireEnv(tb testing.TB, key string) string {
	// TODO: реализуйте функцию
	return ""
}

// CheckDiscount проверяет, что discount(price, percent) == want без ошибки.
func CheckDiscount(tb testing.TB, discount func(int, int) (int, error), price, percent, want int) {
	// TODO: реализуйте функцию
}

// CheckDiscountError проверяет, что discount(price, percent) вернула ошибку target.
func CheckDiscountError(tb testing.TB, discount func(int, int) (int, error), price, percent int, target error) {
	// TODO: реализуйте функцию
}

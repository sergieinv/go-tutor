package task

import "testing"

// Golden сравнивает got с golden-файлом path или перезаписывает его при update.
func Golden(tb testing.TB, path, got string, update bool) {
	// TODO: реализуйте хелпер
}

// Swap присваивает *p = v до конца теста и восстанавливает старое значение.
func Swap[T any](tb testing.TB, p *T, v T) {
	// TODO: реализуйте хелпер
}

// Тесты в Go живут рядом с кодом: `cart.go` и `cart_test.go`
// лежат в одном каталоге, отдельного тестового проекта нет.
// Здесь тестируемый код — пакет `shop` в `main.go`, тесты —
// в `main_test.go` и `internal_test.go`. `go test` собирает
// пакет вместе с `_test.go`-файлами, а `go build` их не видит.

package shop

import (
	"errors"
	"fmt"
	"strings"
)

// Ошибка-значение, которую тесты сравнивают через `errors.Is`.
var ErrBadPercent = errors.New("скидка вне диапазона 0..100")

// Discount возвращает цену в копейках после скидки percent%.
// Дробная часть скидки отбрасывается.
func Discount(price, percent int) (int, error) {
	if percent < 0 || percent > 100 {
		return 0, fmt.Errorf("%w: %d", ErrBadPercent, percent)
	}
	return price - price*percent/100, nil
}

// Неэкспортируемая функция: снаружи пакета её не вызвать,
// поэтому тестирует её внутренний тестовый пакет.
func normalizeSKU(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// Внешний тестовый пакет `shop_test` видит `shop` так же,
// как любой его пользователь: только экспортированное.
// Модуль песочницы называется `lesson`, отсюда импорт
// `shop "lesson"`; в проекте был бы `example.com/shop`.
//
// Запуск: `go test` — тесты пакета, `go test ./...` — всех
// пакетов модуля, `-v` — подробно, `-run 'Discount$'` — только
// тесты, чьё имя подходит под регулярное выражение.
package shop_test

import (
	"errors"
	"os"
	"testing"

	shop "lesson"
)

// Тест — функция `TestXxx(t *testing.T)` в файле `_test.go`.
// Ни атрибутов, ни классов: `go test` находит тесты по имени.
// Ассертов в `testing` нет — обычный `if` и сообщение
// в формате «вызов = получено, want ожидалось».
func TestDiscount(t *testing.T) {
	got, err := shop.Discount(1000, 15)
	if err != nil {
		// `Fatalf` — отметить провал и сразу остановить тест:
		// проверять `got` при ошибке бессмысленно.
		t.Fatalf("Discount(1000, 15): неожиданная ошибка %v", err)
	}
	if got != 850 {
		// `Errorf` — отметить провал и продолжить: за один
		// прогон видны все расхождения, а не только первое.
		t.Errorf("Discount(1000, 15) = %d, want 850", got)
	}
}

// Ошибки проверяют через `errors.Is`, а не по тексту.
func TestDiscountBadPercent(t *testing.T) {
	for _, p := range []int{-1, 101} {
		_, err := shop.Discount(1000, p)
		if !errors.Is(err, shop.ErrBadPercent) {
			t.Errorf("Discount(1000, %d): err = %v, want ErrBadPercent", p, err)
		}
	}
}

// `t.Log` пишет в вывод теста с именем файла и строкой.
// Его видно с `-v` или при провале теста — зелёный
// прогон без `-v` печатает только итог.
func TestDiscountRounding(t *testing.T) {
	got, _ := shop.Discount(999, 50)
	t.Logf("скидка 50%% от 999 = 499: дробная часть отброшена, цена %d", got)
	if got != 500 {
		t.Errorf("Discount(999, 50) = %d, want 500", got)
	}
}

// `t.Skip` пропускает тест с объяснением — например, когда
// нет внешней зависимости. В выводе будет `--- SKIP`.
func TestWithDatabase(t *testing.T) {
	if os.Getenv("SHOP_DSN") == "" {
		t.Skip("SHOP_DSN не задана — интеграционный тест пропущен")
	}
	t.Log("сюда не дойдём: Skip останавливает тест")
}

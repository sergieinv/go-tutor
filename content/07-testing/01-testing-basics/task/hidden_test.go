package task

import (
	"errors"
	"fmt"
	"testing"
)

func TestHiddenRequireEnvMissing(t *testing.T) {
	t.Setenv("TASK_EMPTY_VAR", "")
	reached := false
	calls := run(t, func(tb testing.TB) {
		RequireEnv(tb, "TASK_EMPTY_VAR")
		reached = true
	})
	wantCalls(t, calls, call{"Skipf", "TASK_EMPTY_VAR не задана"})
	if reached {
		t.Error("после пропуска теста RequireEnv вернула управление — используйте tb.Skipf")
	}

	calls = run(t, func(tb testing.TB) { RequireEnv(tb, "TASK_SURELY_UNSET_VARIABLE_42") })
	wantCalls(t, calls, call{"Skipf", "TASK_SURELY_UNSET_VARIABLE_42 не задана"})
}

func TestHiddenCheckDiscountFatalStops(t *testing.T) {
	// Ошибка и «неправильный» результат одновременно: после Fatalf
	// сравнения уже не должно быть.
	broken := func(int, int) (int, error) { return 42, errors.New("база недоступна") }
	calls := run(t, func(tb testing.TB) { CheckDiscount(tb, broken, 500, 10, 450) })
	wantCalls(t, calls, call{"Fatalf", "Discount(500, 10): неожиданная ошибка: база недоступна"})
}

func TestHiddenCheckDiscountCallsOnce(t *testing.T) {
	n := 0
	counting := func(price, percent int) (int, error) {
		n++
		return discount(price, percent)
	}
	run(t, func(tb testing.TB) { CheckDiscount(tb, counting, 200, 50, 100) })
	run(t, func(tb testing.TB) { CheckDiscountError(tb, counting, 200, 150, errBadPercent) })
	if n != 2 {
		t.Errorf("discount вызвана %d раз за две проверки, want 2 — по одному разу на проверку", n)
	}
}

func TestHiddenCheckDiscountZeroValues(t *testing.T) {
	// Нулевой результат — тоже результат: 0 == 0, ошибки нет.
	calls := run(t, func(tb testing.TB) { CheckDiscount(tb, discount, 0, 100, 0) })
	wantCalls(t, calls)
	calls = run(t, func(tb testing.TB) { CheckDiscount(tb, discount, 300, 100, 1) })
	wantCalls(t, calls, call{"Errorf", "Discount(300, 100) = 0, want 1"})
}

func TestHiddenCheckDiscountErrorWrapped(t *testing.T) {
	// Ошибка обёрнута дважды — errors.Is всё равно её находит.
	wrapped := func(price, percent int) (int, error) {
		_, err := discount(price, percent)
		return 0, fmt.Errorf("расчёт: %w", err)
	}
	calls := run(t, func(tb testing.TB) { CheckDiscountError(tb, wrapped, 100, 101, errBadPercent) })
	wantCalls(t, calls)
}

func TestHiddenCheckDiscountErrorWrong(t *testing.T) {
	other := errors.New("нет такого товара")
	wrong := func(int, int) (int, error) { return 0, other }
	calls := run(t, func(tb testing.TB) { CheckDiscountError(tb, wrong, 100, 101, errBadPercent) })
	wantCalls(t, calls, call{"Errorf", "Discount(100, 101): err = нет такого товара, want скидка вне диапазона"})
}

func TestHiddenCheckDiscountErrorMissingOnce(t *testing.T) {
	// Ошибки нет — ровно одно сообщение, без второго «err = <nil>».
	calls := run(t, func(tb testing.TB) { CheckDiscountError(tb, discount, 700, 0, errBadPercent) })
	wantCalls(t, calls, call{"Errorf", "Discount(700, 0) = 700, nil; want error скидка вне диапазона"})
}

package task

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// call — один вызов метода testing.TB, который сделала проверка.
type call struct {
	method string // Errorf, Fatalf или Skipf
	msg    string
}

// fakeTB подменяет настоящий *testing.T: записывает вызовы
// вместо того, чтобы валить тест. Остальные методы интерфейса
// достаются от встроенного TB.
type fakeTB struct {
	testing.TB
	calls []call
}

func (f *fakeTB) Helper()             {}
func (f *fakeTB) Log(args ...any)     {}
func (f *fakeTB) Logf(string, ...any) {}
func (f *fakeTB) Fail()               { f.calls = append(f.calls, call{"Fail", ""}) }
func (f *fakeTB) Errorf(format string, args ...any) {
	f.calls = append(f.calls, call{"Errorf", fmt.Sprintf(format, args...)})
}
func (f *fakeTB) Error(args ...any) { f.calls = append(f.calls, call{"Errorf", sprint(args)}) }

// Fatal и Skip у настоящего T завершают горутину теста — фейк тоже.
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.calls = append(f.calls, call{"Fatalf", fmt.Sprintf(format, args...)})
	runtime.Goexit()
}
func (f *fakeTB) Fatal(args ...any) {
	f.calls = append(f.calls, call{"Fatalf", sprint(args)})
	runtime.Goexit()
}
func (f *fakeTB) FailNow() { f.calls = append(f.calls, call{"FailNow", ""}); runtime.Goexit() }
func (f *fakeTB) Skipf(format string, args ...any) {
	f.calls = append(f.calls, call{"Skipf", fmt.Sprintf(format, args...)})
	runtime.Goexit()
}
func (f *fakeTB) Skip(args ...any) {
	f.calls = append(f.calls, call{"Skipf", sprint(args)})
	runtime.Goexit()
}
func (f *fakeTB) SkipNow() { f.calls = append(f.calls, call{"SkipNow", ""}); runtime.Goexit() }

func sprint(args []any) string { return strings.TrimSuffix(fmt.Sprintln(args...), "\n") }

// run выполняет проверку в отдельной горутине: Fatalf и Skipf
// завершают именно её, как у настоящего теста.
func run(t *testing.T, check func(tb testing.TB)) []call {
	t.Helper()
	f := &fakeTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		check(f)
	}()
	<-done
	return f.calls
}

func wantCalls(t *testing.T, got []call, want ...call) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("вызовы tb:\n got %q\nwant %q", got, want)
	}
}

var errBadPercent = errors.New("скидка вне диапазона")

// discount — правильная реализация для проверок.
func discount(price, percent int) (int, error) {
	if percent < 0 || percent > 100 {
		return 0, fmt.Errorf("%w: %d", errBadPercent, percent)
	}
	return price - price*percent/100, nil
}

func TestCheckDiscountPass(t *testing.T) {
	calls := run(t, func(tb testing.TB) { CheckDiscount(tb, discount, 1000, 15, 850) })
	wantCalls(t, calls)
}

func TestCheckDiscountWrongValue(t *testing.T) {
	calls := run(t, func(tb testing.TB) { CheckDiscount(tb, discount, 1000, 15, 900) })
	wantCalls(t, calls, call{"Errorf", "Discount(1000, 15) = 850, want 900"})
}

func TestCheckDiscountUnexpectedError(t *testing.T) {
	calls := run(t, func(tb testing.TB) { CheckDiscount(tb, discount, 1000, 120, 0) })
	wantCalls(t, calls, call{"Fatalf", "Discount(1000, 120): неожиданная ошибка: скидка вне диапазона: 120"})
}

func TestCheckDiscountErrorPass(t *testing.T) {
	calls := run(t, func(tb testing.TB) { CheckDiscountError(tb, discount, 1000, -5, errBadPercent) })
	wantCalls(t, calls)
}

func TestCheckDiscountErrorMissing(t *testing.T) {
	calls := run(t, func(tb testing.TB) { CheckDiscountError(tb, discount, 1000, 50, errBadPercent) })
	wantCalls(t, calls, call{"Errorf", "Discount(1000, 50) = 500, nil; want error скидка вне диапазона"})
}

func TestRequireEnv(t *testing.T) {
	t.Setenv("SHOP_DSN", "postgres://localhost/shop")
	var got string
	calls := run(t, func(tb testing.TB) { got = RequireEnv(tb, "SHOP_DSN") })
	wantCalls(t, calls)
	if got != "postgres://localhost/shop" {
		t.Errorf("RequireEnv(SHOP_DSN) = %q, want %q", got, "postgres://localhost/shop")
	}
}

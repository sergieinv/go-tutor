package task

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
)

func TestSafeCallWrapsError(t *testing.T) {
	boom := errors.New("boom")
	_, err := SafeCall(func() int { panic(fmt.Errorf("ctx: %w", boom)) })
	if !errors.Is(err, boom) {
		t.Fatalf("errors.Is(err, boom) = false для %v: ошибку паники нужно обернуть через %%w", err)
	}
}

func TestSafeCallRuntimeError(t *testing.T) {
	_, err := SafeCall(func() int {
		var s []int
		return s[3]
	})
	var re runtime.Error
	if !errors.As(err, &re) {
		t.Fatalf("SafeCall = %v, want ошибку, в цепочке которой runtime.Error", err)
	}
}

func TestSafeCallNonErrorValue(t *testing.T) {
	_, err := SafeCall(func() int { panic(42) })
	if err == nil || err.Error() != "panic: 42" {
		t.Fatalf("SafeCall = %v, want \"panic: 42\"", err)
	}
}

func TestSafeCallPanicNil(t *testing.T) {
	got, err := SafeCall(func() int { panic(nil) })
	var pe *runtime.PanicNilError
	if got != 0 || !errors.As(err, &pe) {
		t.Fatalf("SafeCall(panic(nil)) = (%d, %v), want (0, *runtime.PanicNilError)", got, err)
	}
}

func TestSafeCallRepanicsFatal(t *testing.T) {
	fatal := fmt.Errorf("db: %w", ErrFatal)
	var r any
	func() {
		defer func() { r = recover() }()
		SafeCall(func() int { panic(fatal) })
		t.Error("SafeCall вернул управление, а должен был паниковать снова")
	}()
	if r != fatal {
		t.Fatalf("повторная паника со значением %v, want то же значение %v", r, fatal)
	}
}

func TestSafeCallZeroResult(t *testing.T) {
	got, err := SafeCall(func() int {
		defer func() {}()
		panic("late")
	})
	if got != 0 || err == nil {
		t.Fatalf("SafeCall = (%d, %v), want (0, ошибка)", got, err)
	}
}

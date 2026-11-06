package task

import (
	"errors"
	"fmt"
)

// ErrFatal — паники с этой ошибкой SafeCall не перехватывает.
var ErrFatal = errors.New("fatal")

// SafeCall вызывает f и превращает панику в ошибку "panic: ...".
// Паника со значением-ошибкой оборачивается так, чтобы работал
// errors.Is. Паники с ErrFatal пробрасываются дальше.
func SafeCall(f func() int) (result int, err error) {
	defer func() {
		r := recover()
		if r == nil {
			return // паники не было; panic(nil) с Go 1.21 даёт не nil
		}
		if e, ok := r.(error); ok {
			if errors.Is(e, ErrFatal) {
				panic(r) // re-panic: не наша забота
			}
			result, err = 0, fmt.Errorf("panic: %w", e)
			return
		}
		result, err = 0, fmt.Errorf("panic: %v", r)
	}()
	return f(), nil
}

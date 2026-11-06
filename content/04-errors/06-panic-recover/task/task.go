package task

import "errors"

// ErrFatal — паники с этой ошибкой SafeCall не перехватывает.
var ErrFatal = errors.New("fatal")

// SafeCall вызывает f и превращает панику в ошибку "panic: ...".
// Паника со значением-ошибкой оборачивается так, чтобы работал
// errors.Is. Паники с ErrFatal пробрасываются дальше.
func SafeCall(f func() int) (result int, err error) {
	// TODO: реализуйте функцию
	return 0, nil
}

package task

import "errors"

// ErrFormat — текст не похож на профиль горутин в формате debug=1.
var ErrFormat = errors.New("goroutine profile: bad format")

// Site — функция пакета и число горутин, которые стоят в ней.
type Site struct {
	Func  string
	Count int
}

// Blocked разбирает профиль горутин (pprof.Lookup("goroutine").WriteTo(w, 1))
// и для каждой группы одинаковых стеков находит верхнюю функцию пакета pkg.
func Blocked(profile, pkg string) ([]Site, error) {
	// TODO: реализуйте функцию
	return nil, nil
}

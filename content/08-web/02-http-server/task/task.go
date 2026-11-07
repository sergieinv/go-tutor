package task

import "net/http"

// Counter — HTTP-счётчик. Нулевое значение готово к работе.
type Counter struct {
	// TODO: добавьте поля
}

// ServeHTTP обрабатывает GET/HEAD, POST, DELETE.
func (c *Counter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: реализуйте метод
}

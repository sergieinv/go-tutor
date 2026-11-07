package task

import "net/http"

// NewRouter возвращает роутер API заметок с пустым хранилищем.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	// TODO: зарегистрируйте маршруты из условия
	return mux
}

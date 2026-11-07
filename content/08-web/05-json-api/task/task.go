package task

import "net/http"

// User — пользователь API.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Problem — ответ-ошибка в формате RFC 9457.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// NewAPI возвращает API пользователей с пустым хранилищем.
func NewAPI() http.Handler {
	mux := http.NewServeMux()
	// TODO: зарегистрируйте POST /users, GET /users/{id}, DELETE /users/{id}
	return mux
}

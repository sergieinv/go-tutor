package task

// SignupRequest — тело запроса регистрации.
type SignupRequest struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Age      int      `json:"age"`
	Tags     []string `json:"tags"`
}

// FieldErrors — ошибки полей: имя поля в JSON → сообщение.
type FieldErrors map[string]string

// Error возвращает пары «ключ: сообщение», отсортированные по ключу.
func (fe FieldErrors) Error() string {
	// TODO: реализуйте метод
	return ""
}

// Normalize приводит поля к канонической форме.
func (r *SignupRequest) Normalize() {
	// TODO: реализуйте метод
}

// Validate проверяет поля и возвращает nil или FieldErrors.
func (r SignupRequest) Validate() error {
	// TODO: реализуйте метод
	return nil
}

// Status выбирает HTTP-код ответа по ошибке.
func Status(err error) int {
	// TODO: реализуйте функцию
	return 0
}

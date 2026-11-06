package task

import "errors"

var (
	ErrRequired = errors.New("required")
	ErrTooLong  = errors.New("too long")
	ErrInvalid  = errors.New("invalid")
)

type User struct {
	Name  string
	Email string
	Age   int
}

// FieldError — ошибка в конкретном поле.
type FieldError struct {
	Field string // "name", "email" или "age"
	Err   error  // ErrRequired, ErrTooLong или ErrInvalid
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Err.Error() }

// TODO: добавьте метод Unwrap, чтобы errors.Is видел причину.

// Validate проверяет поля u и возвращает все ошибки,
// объединённые через errors.Join, или nil.
func Validate(u User) error {
	// TODO: реализуйте функцию
	return nil
}

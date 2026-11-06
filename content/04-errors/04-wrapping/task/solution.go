package task

import (
	"errors"
	"strings"
)

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

// Unwrap открывает причину для errors.Is и errors.As.
func (e *FieldError) Unwrap() error { return e.Err }

// Validate проверяет поля u и возвращает все ошибки,
// объединённые через errors.Join, или nil.
func Validate(u User) error {
	var errs []error
	add := func(field string, err error) {
		errs = append(errs, &FieldError{Field: field, Err: err})
	}

	switch {
	case u.Name == "":
		add("name", ErrRequired)
	case len(u.Name) > 32:
		add("name", ErrTooLong)
	}
	switch {
	case u.Email == "":
		add("email", ErrRequired)
	case !strings.Contains(u.Email, "@"):
		add("email", ErrInvalid)
	}
	if u.Age < 0 || u.Age > 150 {
		add("age", ErrInvalid)
	}
	// Join пустого списка — nil, отдельная проверка не нужна.
	return errors.Join(errs...)
}

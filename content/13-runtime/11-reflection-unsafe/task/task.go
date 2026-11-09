package task

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidTarget — dst не является ненулевым указателем на структуру.
	ErrInvalidTarget = errors.New("target must be a non-nil pointer to a struct")
	// ErrMissing — обязательная переменная не задана.
	ErrMissing = errors.New("required variable is not set")
	// ErrUnsupported — поле нельзя заполнить: тип не поддерживается,
	// поле не экспортировано или тег некорректен.
	ErrUnsupported = errors.New("unsupported field")
)

// FieldError описывает ошибку конкретного поля.
type FieldError struct {
	Field string // имя поля структуры
	Key   string // имя переменной из тега
	Err   error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("env %s (field %s): %v", e.Key, e.Field, e.Err)
}

func (e *FieldError) Unwrap() error { return e.Err }

// Load заполняет поля структуры *dst из env по тегам `env:"NAME[,required]"`.
func Load(dst any, env map[string]string) error {
	// TODO: reflect.ValueOf(dst), проверка указателя на структуру,
	// обход полей, разбор тега, установка значений
	return nil
}

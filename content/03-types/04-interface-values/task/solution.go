package task

import "fmt"

// FieldError — ошибка проверки поля.
type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Reason }

// FirstError возвращает первый не-nil элемент как error.
// Если таких нет, результат должен быть равен nil.
func FirstError(errs ...*FieldError) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	// Явный nil: вернуть здесь переменную типа *FieldError
	// значило бы вернуть не-nil интерфейс.
	return nil
}

// Kind классифицирует значение: "nil", "int", "string",
// "error", "stringer" или "other".
func Kind(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	case string:
		return "string"
	case error:
		// Ветка error выше Stringer: значение с обоими
		// методами должно считаться ошибкой.
		return "error"
	case fmt.Stringer:
		return "stringer"
	default:
		return "other"
	}
}

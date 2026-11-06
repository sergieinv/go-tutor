package task

// FieldError — ошибка проверки поля.
type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Reason }

// FirstError возвращает первый не-nil элемент как error.
// Если таких нет, результат должен быть равен nil.
func FirstError(errs ...*FieldError) error {
	// TODO: реализуйте функцию
	return &FieldError{}
}

// Kind классифицирует значение: "nil", "int", "string",
// "error", "stringer" или "other".
func Kind(v any) string {
	// TODO: реализуйте функцию
	return ""
}

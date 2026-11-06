package task

import (
	"errors"
	"testing"
)

// fields возвращает ошибки полей из результата Validate по порядку.
func fields(t *testing.T, err error) []*FieldError {
	t.Helper()
	var list []error
	if m, ok := err.(interface{ Unwrap() []error }); ok {
		list = m.Unwrap()
	} else if err != nil {
		list = []error{err}
	}
	var out []*FieldError
	for _, e := range list {
		var fe *FieldError
		if !errors.As(e, &fe) {
			t.Fatalf("ошибка %v — не *FieldError", e)
		}
		out = append(out, fe)
	}
	return out
}

func TestValidateOK(t *testing.T) {
	if err := Validate(User{Name: "Ann", Email: "ann@example.com", Age: 30}); err != nil {
		t.Fatalf("Validate вернул %v, want nil", err)
	}
}

func TestValidateAllErrors(t *testing.T) {
	err := Validate(User{Name: "", Email: "nope", Age: -1})
	if !errors.Is(err, ErrRequired) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate = %v, want ErrRequired и ErrInvalid в цепочке", err)
	}
	got := fields(t, err)
	want := []FieldError{{"name", ErrRequired}, {"email", ErrInvalid}, {"age", ErrInvalid}}
	if len(got) != len(want) {
		t.Fatalf("ошибок %d, want %d: %v", len(got), len(want), err)
	}
	for i := range want {
		if *got[i] != want[i] {
			t.Errorf("ошибка %d = %v, want %v", i, got[i], &want[i])
		}
	}
}

package task

import (
	"errors"
	"strings"
	"testing"
)

func TestFieldErrorUnwrap(t *testing.T) {
	fe := &FieldError{Field: "x", Err: ErrTooLong}
	if !errors.Is(fe, ErrTooLong) {
		t.Fatalf("errors.Is(*FieldError, ErrTooLong) = false: нет метода Unwrap")
	}
}

func TestValidateSingleField(t *testing.T) {
	long := strings.Repeat("a", 33)
	tests := []struct {
		name string
		u    User
		want FieldError
	}{
		{"name too long", User{Name: long, Email: "a@b", Age: 1}, FieldError{"name", ErrTooLong}},
		{"email required", User{Name: "A", Email: "", Age: 1}, FieldError{"email", ErrRequired}},
		{"age too big", User{Name: "A", Email: "a@b", Age: 151}, FieldError{"age", ErrInvalid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fields(t, Validate(tt.u))
			if len(got) != 1 || *got[0] != tt.want {
				t.Fatalf("Validate(%+v) = %v, want ровно одну ошибку %v", tt.u, got, &tt.want)
			}
		})
	}
}

func TestValidateBoundaries(t *testing.T) {
	for _, u := range []User{
		{Name: strings.Repeat("a", 32), Email: "@", Age: 0},
		{Name: "Ж", Email: "x@y", Age: 150},
	} {
		if err := Validate(u); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", u, err)
		}
	}
	// 17 кириллических букв — 34 байта: длина считается в байтах.
	got := fields(t, Validate(User{Name: strings.Repeat("ж", 17), Email: "a@b"}))
	if len(got) != 1 || *got[0] != (FieldError{"name", ErrTooLong}) {
		t.Errorf("имя из 34 байт: %v, want name: too long", got)
	}
}

func TestValidateIsJoined(t *testing.T) {
	err := Validate(User{Name: "", Email: "", Age: 200})
	if _, ok := err.(interface{ Unwrap() []error }); !ok {
		t.Fatalf("Validate должен объединять ошибки через errors.Join, получено %T", err)
	}
	if got := err.Error(); got != "name: required\nemail: required\nage: invalid" {
		t.Errorf("текст = %q", got)
	}
}

func TestValidateReturnsRealNil(t *testing.T) {
	var err error = Validate(User{Name: "A", Email: "a@b", Age: 5})
	if err != nil {
		t.Fatalf("Validate вернул не-nil %#v для корректного пользователя", err)
	}
}

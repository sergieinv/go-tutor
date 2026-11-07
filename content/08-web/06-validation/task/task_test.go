package task

import (
	"errors"
	"reflect"
	"testing"
)

func valid() SignupRequest {
	return SignupRequest{
		Username: "anna_k",
		Email:    "anna@example.com",
		Password: "secret123",
		Age:      30,
		Tags:     []string{"go", "dotnet"},
	}
}

func TestNormalize(t *testing.T) {
	r := SignupRequest{
		Username: "  anna_k ",
		Email:    " Anna@Example.COM ",
		Password: " secret123 ",
		Tags:     []string{" Go ", "go", "", "  ", "DotNet"},
	}
	r.Normalize()
	want := SignupRequest{
		Username: "anna_k",
		Email:    "anna@example.com",
		Password: " secret123 ",
		Tags:     []string{"go", "dotnet"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("после Normalize:\n got %#v\nwant %#v", r, want)
	}
}

func TestValidateOK(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate() корректного запроса = %v, want nil", err)
	}
}

func TestValidateCollectsAll(t *testing.T) {
	err := SignupRequest{Username: "ab", Password: "short", Age: 7}.Validate()
	var fe FieldErrors
	if !errors.As(err, &fe) {
		t.Fatalf("Validate() = %v (%T), want FieldErrors", err, err)
	}
	want := FieldErrors{
		"username": "must be 3-20 characters",
		"email":    "required",
		"password": "must be at least 8 characters",
		"age":      "must be between 13 and 120",
	}
	if !reflect.DeepEqual(fe, want) {
		t.Fatalf("ошибки полей:\n got %v\nwant %v", fe, want)
	}
}

func TestFieldErrorsString(t *testing.T) {
	fe := FieldErrors{"email": "required", "age": "must be between 13 and 120"}
	want := "age: must be between 13 and 120; email: required"
	if got := fe.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 200},
		{"field errors", FieldErrors{"age": "x"}, 422},
		{"other", errors.New("unexpected EOF"), 400},
	}
	for _, tt := range tests {
		if got := Status(tt.err); got != tt.want {
			t.Errorf("Status(%s) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

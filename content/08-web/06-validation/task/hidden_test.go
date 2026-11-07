package task

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func fieldErrors(t *testing.T, r SignupRequest) FieldErrors {
	t.Helper()
	err := r.Validate()
	if err == nil {
		return nil
	}
	var fe FieldErrors
	if !errors.As(err, &fe) {
		t.Fatalf("Validate() = %v (%T), want FieldErrors", err, err)
	}
	return fe
}

func TestValidateNilWhenValid(t *testing.T) {
	// Ловушка: пустая FieldErrors в интерфейсе error — не nil.
	r := valid()
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate() = %#v, want nil (верните nil явно, а не пустую FieldErrors)", err)
	}
	r.Tags = nil
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate() без тегов = %v, want nil", err)
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		username string
		want     string // "" — ошибки нет
	}{
		{"", "required"},
		{"ab", "must be 3-20 characters"},
		{"abc", ""},
		{"a_b_c_d_e_f_g_h_i_j_", ""}, // 20
		{"a_b_c_d_e_f_g_h_i_j_k", "must be 3-20 characters"}, // 21
		{"a!", "must be 3-20 characters"},                    // сначала длина
		{"ab!", "invalid characters"},
		{"anna k", "invalid characters"},
		{"Anna_K_99", ""},
		{"ёж", "must be 3-20 characters"},       // 2 символа, хотя 4 байта
		{"ёжикёжикёжикё", "invalid characters"}, // 13 символов, 26 байт
		{"anna-k", "invalid characters"},
	}
	for _, tt := range tests {
		r := valid()
		r.Username = tt.username
		if got := fieldErrors(t, r)["username"]; got != tt.want {
			t.Errorf("username %q: ошибка %q, want %q", tt.username, got, tt.want)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{"", "required"},
		{"bob@x.io", ""},
		{"bob", "invalid email"},
		{"@x.io", "invalid email"},
		{"Bob <bob@x.io>", "invalid email"},
		{"<bob@x.io>", "invalid email"},
		{"bob@x.io, eve@x.io", "invalid email"},
	}
	for _, tt := range tests {
		r := valid()
		r.Email = tt.email
		if got := fieldErrors(t, r)["email"]; got != tt.want {
			t.Errorf("email %q: ошибка %q, want %q", tt.email, got, tt.want)
		}
	}
}

func TestValidatePasswordRunes(t *testing.T) {
	tests := []struct {
		password string
		want     string
	}{
		{"", "required"},
		{"1234567", "must be at least 8 characters"},
		{"12345678", ""},
		{"пароль1", "must be at least 8 characters"}, // 7 символов, 13 байт
		{"пароль12", ""},
		{"        ", ""}, // пароль не нормализуется и не обрезается
	}
	for _, tt := range tests {
		r := valid()
		r.Password = tt.password
		if got := fieldErrors(t, r)["password"]; got != tt.want {
			t.Errorf("password %q: ошибка %q, want %q", tt.password, got, tt.want)
		}
	}
}

func TestValidateAge(t *testing.T) {
	for age, want := range map[int]string{
		-1: "must be between 13 and 120", 0: "must be between 13 and 120",
		12: "must be between 13 and 120", 13: "", 120: "", 121: "must be between 13 and 120",
	} {
		r := valid()
		r.Age = age
		if got := fieldErrors(t, r)["age"]; got != want {
			t.Errorf("age %d: ошибка %q, want %q", age, got, want)
		}
	}
}

func TestValidateTags(t *testing.T) {
	long := strings.Repeat("я", 21)
	r := valid()
	r.Tags = []string{"a", "b", "c", "d", "e", "f", long}
	fe := fieldErrors(t, r)
	want := FieldErrors{"tags": "at most 5 tags", "tags[6]": "must be at most 20 characters"}
	if !reflect.DeepEqual(fe, want) {
		t.Fatalf("ошибки:\n got %v\nwant %v", fe, want)
	}

	r.Tags = []string{"a", "b", "c", "d", "e"}
	if fe := fieldErrors(t, r); fe != nil {
		t.Errorf("5 тегов: %v, want без ошибок", fe)
	}
	r.Tags = []string{strings.Repeat("я", 20), long, "ok", long}
	fe = fieldErrors(t, r)
	want = FieldErrors{"tags[1]": "must be at most 20 characters", "tags[3]": "must be at most 20 characters"}
	if !reflect.DeepEqual(fe, want) {
		t.Fatalf("длинные теги:\n got %v\nwant %v", fe, want)
	}
}

func TestValidateDoesNotNormalize(t *testing.T) {
	r := valid()
	r.Email = " Anna@Example.com"
	r.Username = " anna"
	fe := fieldErrors(t, r)
	if fe["email"] != "invalid email" || fe["username"] != "invalid characters" {
		t.Fatalf("Validate не должна нормализовать: %v", fe)
	}
	if r.Email != " Anna@Example.com" {
		t.Fatalf("Validate изменила поле: %q", r.Email)
	}
}

func TestNormalizeThenValidate(t *testing.T) {
	r := SignupRequest{
		Username: "\tanna_k\n",
		Email:    "  BOB@X.IO  ",
		Password: "12345678",
		Age:      20,
		Tags:     []string{"A", "a", " b ", "B", "", "c", "d", "e", "F"},
	}
	r.Normalize()
	if want := []string{"a", "b", "c", "d", "e", "f"}; !reflect.DeepEqual(r.Tags, want) {
		t.Fatalf("Tags = %q, want %q", r.Tags, want)
	}
	want := FieldErrors{"tags": "at most 5 tags"}
	if fe := fieldErrors(t, r); !reflect.DeepEqual(fe, want) {
		t.Fatalf("ошибки: %v, want %v", fe, want)
	}

	r = SignupRequest{Tags: []string{" ", ""}}
	r.Normalize()
	if len(r.Tags) != 0 {
		t.Fatalf("Tags = %q, want пусто", r.Tags)
	}
	r = SignupRequest{}
	r.Normalize()
	if len(r.Tags) != 0 || r.Username != "" || r.Email != "" {
		t.Fatalf("Normalize пустого запроса: %#v", r)
	}
}

func TestFieldErrorsStringHidden(t *testing.T) {
	fe := FieldErrors{"username": "required", "tags[1]": "x", "tags": "y", "age": "z"}
	want := "age: z; tags: y; tags[1]: x; username: required"
	if got := fe.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got := (FieldErrors{"email": "required"}).Error(); got != "email: required" {
		t.Fatalf("Error() одной ошибки = %q", got)
	}
}

func TestStatusWrapped(t *testing.T) {
	fe := FieldErrors{"age": "must be between 13 and 120"}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"wrapped %w", fmt.Errorf("signup: %w", fe), 422},
		{"joined", errors.Join(errors.New("other"), fe), 422},
		{"wrapped %v", fmt.Errorf("signup: %v", fe), 400},
		{"from Validate", SignupRequest{}.Validate(), 422},
		{"valid", valid().Validate(), 200},
	}
	for _, tt := range tests {
		if got := Status(tt.err); got != tt.want {
			t.Errorf("Status(%s) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

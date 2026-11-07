package task

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"sort"
	"strings"
	"unicode/utf8"
)

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
	keys := make([]string, 0, len(fe))
	for k := range fe {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + fe[k]
	}
	return strings.Join(parts, "; ")
}

// Normalize приводит поля к канонической форме.
func (r *SignupRequest) Normalize() {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))

	seen := make(map[string]bool, len(r.Tags))
	tags := r.Tags[:0:0] // новый срез: не портим массив вызывающего
	for _, t := range r.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		tags = append(tags, t)
	}
	r.Tags = tags
}

func validUsernameChars(s string) bool {
	for _, c := range s {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// Validate проверяет поля и возвращает nil или FieldErrors.
func (r SignupRequest) Validate() error {
	errs := FieldErrors{}

	switch n := utf8.RuneCountInString(r.Username); {
	case r.Username == "":
		errs["username"] = "required"
	case n < 3 || n > 20:
		errs["username"] = "must be 3-20 characters"
	case !validUsernameChars(r.Username):
		errs["username"] = "invalid characters"
	}

	if r.Email == "" {
		errs["email"] = "required"
	} else if a, err := mail.ParseAddress(r.Email); err != nil || a.Address != r.Email {
		errs["email"] = "invalid email"
	}

	switch {
	case r.Password == "":
		errs["password"] = "required"
	case utf8.RuneCountInString(r.Password) < 8:
		errs["password"] = "must be at least 8 characters"
	}

	if r.Age < 13 || r.Age > 120 {
		errs["age"] = "must be between 13 and 120"
	}

	if len(r.Tags) > 5 {
		errs["tags"] = "at most 5 tags"
	}
	for i, t := range r.Tags {
		if utf8.RuneCountInString(t) > 20 {
			errs[fmt.Sprintf("tags[%d]", i)] = "must be at most 20 characters"
		}
	}

	// Пустая карта в интерфейсе error — не nil: возвращаем nil явно.
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Status выбирает HTTP-код ответа по ошибке.
func Status(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var fe FieldErrors
	if errors.As(err, &fe) {
		return http.StatusUnprocessableEntity
	}
	return http.StatusBadRequest
}

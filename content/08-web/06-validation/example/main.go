// В Go нет атрибутов валидации и `ModelState`: DTO
// проверяет себя сам методом `Validate() error`. Хендлер
// различает две ошибки клиента: JSON не разобрался — 400,
// разобрался, но значения не годятся — 422 со списком полей.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

// Ошибки полей собираем все сразу, а не до первой: клиенту
// удобнее исправить форму за один заход. Тип реализует
// `error`, поэтому его можно вернуть и найти через `errors.As`.
type FieldErrors map[string]string

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

type CreateUser struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Website string `json:"website"`
}

// Сначала нормализация: обрезать пробелы, привести email
// к нижнему регистру. Иначе `" bob@x.io "` пройдёт одну
// проверку и сломает уникальный индекс в базе.
func (u *CreateUser) Normalize() {
	u.Name = strings.TrimSpace(u.Name)
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	u.Website = strings.TrimSpace(u.Website)
}

// Длина — в символах (`utf8.RuneCountInString`), а не в
// байтах: `len("Анна")` равно 8. `mail.ParseAddress` принимает
// и `"Bob <bob@x.io>"`, поэтому сверяем адрес с исходной
// строкой. `url.Parse` «разбирает» почти всё — схему и хост
// проверяем сами.
func (u CreateUser) Validate() error {
	errs := FieldErrors{}
	if n := utf8.RuneCountInString(u.Name); n < 2 || n > 50 {
		errs["name"] = "должно быть от 2 до 50 символов"
	}
	if a, err := mail.ParseAddress(u.Email); err != nil || a.Address != u.Email {
		errs["email"] = "некорректный email"
	}
	if u.Website != "" {
		p, err := url.Parse(u.Website)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			errs["website"] = "нужен адрес http(s)://..."
		}
	}
	// Пустая карта — это «ошибок нет»: возвращаем nil явно.
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func createUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in CreateUser
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "некорректный JSON", http.StatusBadRequest)
		return
	}
	in.Normalize()
	// `errors.As` найдёт `FieldErrors`, даже если `Validate`
	// обернула её через `%w` или собрала через `errors.Join`.
	var fe FieldErrors
	if err := in.Validate(); errors.As(err, &fe) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"errors": fe})
		return
	}
	fmt.Fprintf(w, "создан %s <%s>\n", in.Name, in.Email)
}

func main() {
	bodies := []string{
		`{"name": " Анна ", "email": " Anna@Example.COM "}`,
		`{"name": "Я", "email": "Bob <bob@x.io>", "website": "javascript:alert(1)"}`,
		`{"name": "Анна", "email": `,
	}
	for _, body := range bodies {
		req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
		rec := httptest.NewRecorder()
		createUser(rec, req)
		fmt.Print(rec.Code, " ", rec.Body.String())
	}

	// Сама ошибка печатается одной строкой — удобно для логов.
	fmt.Println(CreateUser{Name: "Ян", Email: "@"}.Validate())
}

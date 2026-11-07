package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// User — пользователь API.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Problem — ответ-ошибка в формате RFC 9457.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type api struct {
	mu     sync.Mutex
	nextID int
	users  map[int]User
}

// NewAPI возвращает API пользователей с пустым хранилищем.
func NewAPI() http.Handler {
	a := &api{nextID: 1, users: map[int]User{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /users", a.create)
	mux.HandleFunc("GET /users/{id}", a.get)
	mux.HandleFunc("DELETE /users/{id}", a.delete)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail})
}

// decodeJSON строго читает один JSON-объект и возвращает код ошибки.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) (int, error) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return http.StatusUnsupportedMediaType, errors.New("Content-Type must be application/json")
	}
	// Сначала всё тело с лимитом: превышение — 413 независимо от того,
	// где в теле синтаксическая ошибка.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return http.StatusRequestEntityTooLarge, errors.New("body too large")
		}
		return http.StatusBadRequest, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return http.StatusBadRequest, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return http.StatusBadRequest, errors.New("unexpected data after object")
	}
	return 0, nil
}

func validEmail(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@") &&
		strings.IndexFunc(s, unicode.IsSpace) < 0
}

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	// Указатель на структуру: JSON null оставит его nil.
	var in *struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if status, err := decodeJSON(w, r, &in); err != nil {
		writeProblem(w, status, err.Error())
		return
	}
	if in == nil {
		writeProblem(w, http.StatusBadRequest, "body must be a JSON object")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		writeProblem(w, http.StatusUnprocessableEntity, "name must be 1..100 characters")
		return
	}
	if !validEmail(in.Email) {
		writeProblem(w, http.StatusUnprocessableEntity, "invalid email")
		return
	}

	a.mu.Lock()
	for _, u := range a.users {
		if strings.EqualFold(u.Email, in.Email) {
			a.mu.Unlock()
			writeProblem(w, http.StatusConflict, "email already registered")
			return
		}
	}
	u := User{ID: a.nextID, Name: name, Email: in.Email}
	a.nextID++
	a.users[u.ID] = u
	a.mu.Unlock()

	w.Header().Set("Location", "/users/"+strconv.Itoa(u.ID))
	writeJSON(w, http.StatusCreated, u)
}

func userID(r *http.Request) (int, bool) {
	s := r.PathValue("id")
	id, err := strconv.Atoi(s)
	if err != nil || id < 1 || strconv.Itoa(id) != s {
		return 0, false
	}
	return id, true
}

func (a *api) get(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(r)
	a.mu.Lock()
	u, found := a.users[id]
	a.mu.Unlock()
	if !ok || !found {
		writeProblem(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *api) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := userID(r)
	a.mu.Lock()
	_, found := a.users[id]
	delete(a.users, id)
	a.mu.Unlock()
	if !ok || !found {
		writeProblem(w, http.StatusNotFound, "user not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

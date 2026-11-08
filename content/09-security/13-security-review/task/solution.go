package task

import (
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Note — заметка пользователя.
type Note struct {
	ID    int64  `json:"id"`
	Owner string `json:"owner"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Service — сервис заметок: SQL-хранилище и каталог вложений.
type Service struct {
	db    *sql.DB
	files string            // каталог вложений
	users map[string]string // токен → имя пользователя
}

const schema = `CREATE TABLE IF NOT EXISTS notes (
	id    INTEGER PRIMARY KEY,
	owner TEXT NOT NULL,
	title TEXT NOT NULL,
	body  TEXT NOT NULL
)`

// New создаёт таблицу и возвращает сервис.
func New(db *sql.DB, files string, tokens map[string]string) (*Service, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Service{db: db, files: files, users: tokens}, nil
}

// Handler возвращает маршруты сервиса.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notes", s.auth(s.createNote))
	mux.HandleFunc("GET /notes", s.auth(s.listNotes))
	mux.HandleFunc("GET /notes/{id}", s.auth(s.getNote))
	mux.HandleFunc("GET /notes/{id}/html", s.auth(s.noteHTML))
	mux.HandleFunc("GET /files", s.auth(s.download))
	return mux
}

type userHandler func(w http.ResponseWriter, r *http.Request, user string)

// auth пропускает запрос с известным токеном в Authorization: Bearer.
func (s *Service) auth(next userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		user, known := s.users[token]
		if !ok || !known {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, user)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// internal отвечает 500 на непредвиденную ошибку: подробности — в лог,
// клиенту — общий текст.
func internal(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

const maxBody = 64 << 10

func (s *Service) createNote(w http.ResponseWriter, r *http.Request, user string) {
	var in struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if n := utf8.RuneCountInString(in.Title); n < 1 || n > 200 || utf8.RuneCountInString(in.Body) > 10000 {
		http.Error(w, "invalid note", http.StatusBadRequest)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO notes (owner, title, body) VALUES (?, ?, ?)`, user, in.Title, in.Body)
	if err != nil {
		internal(w, err)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, Note{ID: id, Owner: user, Title: in.Title, Body: in.Body})
}

func (s *Service) listNotes(w http.ResponseWriter, r *http.Request, user string) {
	// Имя столбца плейсхолдером не передать — только allowlist.
	orderBy, ok := map[string]string{"": "id", "id": "id", "title": "title"}[r.URL.Query().Get("sort")]
	if !ok {
		http.Error(w, "invalid sort", http.StatusBadRequest)
		return
	}
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, owner, title, body FROM notes WHERE owner = ? ORDER BY `+orderBy, user)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	notes := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Owner, &n.Title, &n.Body); err != nil {
			internal(w, err)
			return
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

var errNotFound = errors.New("note not found")

// note загружает заметку пользователя по id из пути запроса.
// Чужая заметка неотличима от несуществующей.
func (s *Service) note(r *http.Request, user string) (Note, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return Note{}, errNotFound
	}
	var n Note
	err = s.db.QueryRowContext(r.Context(),
		`SELECT id, owner, title, body FROM notes WHERE id = ? AND owner = ?`, id, user).
		Scan(&n.ID, &n.Owner, &n.Title, &n.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, errNotFound
	}
	return n, err
}

func (s *Service) getNote(w http.ResponseWriter, r *http.Request, user string) {
	n, err := s.note(r, user)
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case err != nil:
		internal(w, err)
	default:
		writeJSON(w, http.StatusOK, n)
	}
}

var page = template.Must(template.New("note").Parse(
	`<!doctype html><title>{{.Title}}</title><h1>{{.Title}}</h1><div>{{.Body}}</div>`))

func (s *Service) noteHTML(w http.ResponseWriter, r *http.Request, user string) {
	n, err := s.note(r, user)
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case err != nil:
		internal(w, err)
		return
	}
	// Сначала экранируем ввод, потом добавляем свою разметку.
	body := template.HTML(strings.ReplaceAll(template.HTMLEscapeString(n.Body), "\n", "<br>"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Execute(w, struct {
		Title string
		Body  template.HTML
	}{n.Title, body})
}

func (s *Service) download(w http.ResponseWriter, r *http.Request, user string) {
	name := r.URL.Query().Get("name")
	// os.Root не выпускает за каталог ни через "..", ни через
	// символические ссылки. Всё, что нельзя отдать, — 404.
	root, err := os.OpenRoot(s.files)
	if err != nil {
		internal(w, err)
		return
	}
	defer root.Close()
	info, err := root.Stat(name)
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	data, err := root.ReadFile(name)
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

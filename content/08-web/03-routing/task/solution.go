package task

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type store struct {
	mu     sync.Mutex
	nextID int
	notes  map[int]string
}

// NewRouter возвращает роутер API заметок с пустым хранилищем.
func NewRouter() http.Handler {
	s := &store{nextID: 1, notes: map[int]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "notes api\n")
	})
	mux.HandleFunc("GET admin.local/{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "admin\n")
	})
	mux.HandleFunc("GET /notes/{$}", s.list)
	mux.HandleFunc("POST /notes/{$}", s.create)
	mux.HandleFunc("GET /notes/count", s.count)
	mux.HandleFunc("GET /notes/{id}", s.get)
	mux.HandleFunc("DELETE /notes/{id}", s.delete)
	mux.HandleFunc("GET /raw/{path...}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "raw: %s\n", r.PathValue("path"))
	})
	return mux
}

// noteID разбирает {id}: только положительное десятичное целое.
func noteID(r *http.Request) (int, bool) {
	s := r.PathValue("id")
	id, err := strconv.Atoi(s)
	if err != nil || id < 1 || strconv.Itoa(id) != s {
		return 0, false
	}
	return id, true
}

func (s *store) list(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range slices.Sorted(maps.Keys(s.notes)) {
		fmt.Fprintf(w, "%d: %s\n", id, s.notes[id])
	}
}

func (s *store) create(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		http.Error(w, "empty note", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.notes[id] = text
	s.mu.Unlock()
	w.Header().Set("Location", "/notes/"+strconv.Itoa(id))
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "%d\n", id)
}

func (s *store) count(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.notes)
	s.mu.Unlock()
	fmt.Fprintf(w, "%d\n", n)
}

func (s *store) get(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(r)
	s.mu.Lock()
	text, found := s.notes[id]
	s.mu.Unlock()
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	fmt.Fprintf(w, "%s\n", text)
}

func (s *store) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := noteID(r)
	s.mu.Lock()
	_, found := s.notes[id]
	delete(s.notes, id)
	s.mu.Unlock()
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

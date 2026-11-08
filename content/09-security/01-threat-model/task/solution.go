package task

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Doc — документ пользователя.
type Doc struct {
	ID    int64  `json:"id"`
	Owner string `json:"owner"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// User — владелец токена.
type User struct {
	Name  string
	Admin bool
}

type api struct {
	tokens map[string]User

	mu     sync.Mutex
	nextID int64
	docs   map[int64]Doc
}

// NewAPI возвращает обработчик API документов; tokens — токен → пользователь.
func NewAPI(tokens map[string]User) http.Handler {
	a := &api{tokens: maps.Clone(tokens), docs: map[int64]Doc{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /docs", a.list)
	mux.HandleFunc("POST /docs", a.create)
	mux.HandleFunc("GET /docs/{id}", a.get)
	mux.HandleFunc("DELETE /docs/{id}", a.remove)
	mux.HandleFunc("GET /admin/docs", a.adminList)
	return a.authn(mux)
}

type userKey struct{}

// authn пускает дальше только запросы с известным токеном.
func (a *api) authn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		u, known := a.tokens[token]
		if !ok || !known {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}

func userFrom(r *http.Request) User {
	u, _ := r.Context().Value(userKey{}).(User)
	return u
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// sorted возвращает документы, для которых keep вернул true, по возрастанию id.
func (a *api) sorted(keep func(Doc) bool) []Doc {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []Doc{}
	for _, d := range a.docs {
		if keep(d) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(x, y Doc) int { return cmp.Compare(x.ID, y.ID) })
	return out
}

// docID разбирает {id} из пути: только положительные десятичные числа.
func docID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	writeJSON(w, http.StatusOK, a.sorted(func(d Doc) bool { return d.Owner == u.Name }))
}

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var d Doc
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Владелец — всегда тот, кто создаёт: поле owner из тела
	// перезаписываем, а не доверяем ему.
	d.Owner = userFrom(r).Name
	a.mu.Lock()
	a.nextID++
	d.ID = a.nextID
	a.docs[d.ID] = d
	a.mu.Unlock()
	writeJSON(w, http.StatusCreated, d)
}

// ownDoc возвращает документ, только если он принадлежит u.
// Чужой документ неотличим от несуществующего.
func (a *api) ownDoc(u User, id int64) (Doc, bool) {
	d, found := a.docs[id]
	return d, found && d.Owner == u.Name
}

func (a *api) get(w http.ResponseWriter, r *http.Request) {
	id, ok := docID(r)
	a.mu.Lock()
	d, found := a.ownDoc(userFrom(r), id)
	a.mu.Unlock()
	if !ok || !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *api) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := docID(r)
	a.mu.Lock()
	_, found := a.ownDoc(userFrom(r), id)
	if found {
		delete(a.docs, id)
	}
	a.mu.Unlock()
	if !ok || !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminList — все документы. authn проверил только, что
// пользователь известен; право на функцию проверяем здесь.
func (a *api) adminList(w http.ResponseWriter, r *http.Request) {
	if !userFrom(r).Admin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, a.sorted(func(Doc) bool { return true }))
}

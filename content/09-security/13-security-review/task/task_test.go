package task

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

const (
	alice = "tok-alice"
	bob   = "tok-bob"
)

type env struct {
	h     http.Handler
	db    *sql.DB
	dir   string // каталог вложений
	outer string // его родитель: сюда кладём то, что отдавать нельзя
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	outer := t.TempDir()
	dir := filepath.Join(outer, "files")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("quarterly report"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, dir, map[string]string{alice: "alice", bob: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	return &env{h: s.Handler(), db: db, dir: dir, outer: outer}
}

func (e *env) do(method, path, token, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) create(t *testing.T, token, title, body string) Note {
	t.Helper()
	in, _ := json.Marshal(map[string]string{"title": title, "body": body})
	rec := e.do("POST", "/notes", token, string(in))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /notes: %d %s", rec.Code, rec.Body.String())
	}
	var n Note
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil {
		t.Fatalf("ответ POST /notes не JSON: %s", rec.Body.String())
	}
	return n
}

func decodeNotes(t *testing.T, rec *httptest.ResponseRecorder) []Note {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("GET /notes: %d %s", rec.Code, rec.Body.String())
	}
	var notes []Note
	if err := json.Unmarshal(rec.Body.Bytes(), &notes); err != nil {
		t.Fatalf("ответ GET /notes не JSON-массив: %s", rec.Body.String())
	}
	return notes
}

func titles(notes []Note) string {
	var s []string
	for _, n := range notes {
		s = append(s, n.Title)
	}
	return strings.Join(s, ",")
}

func TestCreateAndGet(t *testing.T) {
	e := newEnv(t)
	n := e.create(t, alice, "Покупки", "молоко\nхлеб")
	if n.ID == 0 || n.Owner != "alice" || n.Title != "Покупки" || n.Body != "молоко\nхлеб" {
		t.Fatalf("создана %+v", n)
	}
	rec := e.do("GET", "/notes/"+itoa(n.ID), alice, "")
	var got Note
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got != n {
		t.Fatalf("GET своей заметки: %d %s, want %+v", rec.Code, rec.Body.String(), n)
	}
}

func TestListOwnSorted(t *testing.T) {
	e := newEnv(t)
	e.create(t, alice, "b", "")
	e.create(t, bob, "чужая", "")
	e.create(t, alice, "a", "")
	if got := titles(decodeNotes(t, e.do("GET", "/notes", alice, ""))); got != "b,a" {
		t.Errorf("GET /notes = %q, want \"b,a\" (свои, по id)", got)
	}
	if got := titles(decodeNotes(t, e.do("GET", "/notes?sort=title", alice, ""))); got != "a,b" {
		t.Errorf("GET /notes?sort=title = %q, want \"a,b\"", got)
	}
}

func TestNoteHTML(t *testing.T) {
	e := newEnv(t)
	n := e.create(t, alice, "<План>", "первое\nвторое")
	rec := e.do("GET", "/notes/"+itoa(n.ID)+"/html", alice, "")
	b := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET html: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(b, "<h1>&lt;План&gt;</h1>") || !strings.Contains(b, "первое<br>второе") {
		t.Fatalf("страница %s: want заголовок экранирован, переводы строк — <br>", b)
	}
}

func TestDownload(t *testing.T) {
	e := newEnv(t)
	if rec := e.do("GET", "/files?name=report.txt", alice, ""); rec.Code != 200 || rec.Body.String() != "quarterly report" {
		t.Fatalf("GET /files?name=report.txt: %d %q", rec.Code, rec.Body.String())
	}
	if rec := e.do("GET", "/files?name=missing.txt", alice, ""); rec.Code != 404 {
		t.Fatalf("нет файла: %d, want 404", rec.Code)
	}
}

func TestUnauthorized(t *testing.T) {
	e := newEnv(t)
	for _, tok := range []string{"", "tok-mallory"} {
		if rec := e.do("GET", "/notes", tok, ""); rec.Code != 401 {
			t.Errorf("токен %q: %d, want 401", tok, rec.Code)
		}
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

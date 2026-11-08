package task

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		body string
		want int
	}{
		{`{"title": "x"`, 400},
		{`{"title": "", "body": "b"}`, 400},
		{`{"title": "` + strings.Repeat("я", 201) + `"}`, 400},
		{`{"title": "` + strings.Repeat("я", 200) + `"}`, 201},
		{`{"title": "t", "body": "` + strings.Repeat("ж", 10001) + `"}`, 400},
		{`{"title": "t", "body": "` + strings.Repeat("ж", 10000) + `"}`, 201}, // 20 КБ — в лимите тела
	}
	for _, tt := range tests {
		if rec := e.do("POST", "/notes", alice, tt.body); rec.Code != tt.want {
			t.Errorf("POST %.40q…: %d, want %d", tt.body, rec.Code, tt.want)
		}
	}
}

func TestMissingNote(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/notes/999", "/notes/abc", "/notes/999/html"} {
		if rec := e.do("GET", p, alice, ""); rec.Code != 404 {
			t.Errorf("GET %s: %d, want 404", p, rec.Code)
		}
	}
	if got := decodeNotes(t, e.do("GET", "/notes", bob, "")); len(got) != 0 {
		t.Errorf("без заметок: %v, want []", got)
	}
}

func TestExploitIDOR(t *testing.T) {
	e := newEnv(t)
	n := e.create(t, alice, "пароли", "банк: alice-pin-4321")
	for _, p := range []string{"/notes/" + itoa(n.ID), "/notes/" + itoa(n.ID) + "/html"} {
		rec := e.do("GET", p, bob, "")
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "alice-pin") {
			t.Errorf("bob читает заметку alice через %s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
}

func TestExploitSortInjection(t *testing.T) {
	e := newEnv(t)
	e.create(t, alice, "first", "")
	e.create(t, alice, "second", "")
	e.create(t, bob, "bob", "bob-secret-token")
	// Слепая инъекция: порядок своих заметок отвечает на вопрос
	// «есть ли в чужих заметках такой текст».
	probe := func(guess string) (int, string) {
		sort := "CASE WHEN (SELECT count(*) FROM notes WHERE body LIKE '" + guess + "%') > 0 THEN id ELSE -id END"
		rec := e.do("GET", "/notes?sort="+url.QueryEscape(sort), alice, "")
		if rec.Code != 200 {
			return rec.Code, ""
		}
		return 200, titles(decodeNotes(t, rec))
	}
	codeHit, hit := probe("bob-secret")
	codeMiss, miss := probe("zzz")
	if codeHit == 200 && codeMiss == 200 && hit != miss {
		t.Fatalf("по порядку %q / %q видно, есть ли текст в чужих заметках: sort попал в SQL", hit, miss)
	}
	for _, sort := range []string{"title; DROP TABLE notes", "(SELECT body FROM notes)"} {
		e.do("GET", "/notes?sort="+url.QueryEscape(sort), alice, "")
	}
	if got := titles(decodeNotes(t, e.do("GET", "/notes", alice, ""))); got != "first,second" {
		t.Fatalf("после атак GET /notes = %q", got)
	}
}

func TestExploitStoredXSS(t *testing.T) {
	e := newEnv(t)
	n := e.create(t, alice, "заметка", "привет\n<script>alert(document.cookie)</script><img src=x onerror=alert(1)>")
	rec := e.do("GET", "/notes/"+itoa(n.ID)+"/html", alice, "")
	b := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("GET html: %d", rec.Code)
	}
	if strings.Contains(b, "<script>") || strings.Contains(b, "<img") {
		t.Fatalf("тело заметки вставлено в HTML без экранирования: %s", b)
	}
	if !strings.Contains(b, "привет<br>") {
		t.Fatalf("переводы строк должны остаться <br>: %s", b)
	}
}

func TestExploitPathTraversal(t *testing.T) {
	e := newEnv(t)
	const secret = "DB_PASSWORD=hunter2"
	if err := os.WriteFile(filepath.Join(e.outer, "secret.env"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ссылка внутри каталога вложений, ведущая наружу.
	if err := os.Symlink(filepath.Join(e.outer, "secret.env"), filepath.Join(e.dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../secret.env", "./../secret.env", "sub/../../secret.env", "link.txt", filepath.Join(e.outer, "secret.env")} {
		rec := e.do("GET", "/files?name="+url.QueryEscape(name), alice, "")
		if rec.Code == 200 || strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("name=%q: %d %q — файл вне каталога вложений отдан", name, rec.Code, rec.Body.String())
		}
	}
}

func TestExploitErrorDetails(t *testing.T) {
	e := newEnv(t)
	// Каталог вместо файла и закрытая база — внутренние ошибки.
	for _, p := range []string{"/files?name=", "/files?name=."} {
		rec := e.do("GET", p, alice, "")
		if strings.Contains(rec.Body.String(), e.dir) || strings.Contains(rec.Body.String(), "directory") {
			t.Errorf("GET %s раскрывает внутреннюю ошибку: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	e.db.Close()
	rec := e.do("GET", "/notes", alice, "")
	if rec.Code != 500 {
		t.Fatalf("база закрыта: %d, want 500", rec.Code)
	}
	if b := rec.Body.String(); strings.Contains(b, "sql") || strings.Contains(b, "closed") {
		t.Fatalf("ответ раскрывает ошибку базы: %q", b)
	}
}

func TestExploitHugeBody(t *testing.T) {
	e := newEnv(t)
	body, _ := json.Marshal(map[string]string{"title": "t", "body": "b", "pad": strings.Repeat("A", 1<<20)})
	rec := e.do("POST", "/notes", alice, string(body))
	if rec.Code != 413 {
		t.Fatalf("тело 1 МиБ: %d, want 413", rec.Code)
	}
	if got := decodeNotes(t, e.do("GET", "/notes", alice, "")); len(got) != 0 {
		t.Fatalf("заметка из огромного запроса сохранена: %d шт.", len(got))
	}
}

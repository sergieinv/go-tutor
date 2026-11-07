package task

import (
	"strings"
	"testing"
)

func TestFullFlow(t *testing.T) {
	h := NewRouter()
	steps := []struct {
		method, target, body string
		code                 int
		want                 string
		location             string
	}{
		{"GET", "/notes/", "", 200, "", ""},
		{"GET", "/notes/count", "", 200, "0\n", ""},
		{"POST", "/notes/", "  первая \n", 201, "1\n", "/notes/1"},
		{"POST", "/notes/", "вторая", 201, "2\n", "/notes/2"},
		{"POST", "/notes/", "третья", 201, "3\n", "/notes/3"},
		{"GET", "/notes/1", "", 200, "первая\n", ""},
		{"DELETE", "/notes/2", "", 204, "", ""},
		{"DELETE", "/notes/2", "", 404, "404 page not found\n", ""},
		{"GET", "/notes/2", "", 404, "404 page not found\n", ""},
		{"POST", "/notes/", "четвёртая", 201, "4\n", "/notes/4"}, // id не переиспользуются
		{"GET", "/notes/", "", 200, "1: первая\n3: третья\n4: четвёртая\n", ""},
		{"GET", "/notes/count", "", 200, "3\n", ""},
		{"HEAD", "/notes/count", "", 200, "3\n", ""},
		{"DELETE", "/notes/count", "", 404, "404 page not found\n", ""},
	}
	for _, s := range steps {
		r := call(h, s.method, s.target, s.body)
		if r.code != s.code || r.body != s.want || r.header.Get("Location") != s.location {
			t.Fatalf("%s %s %q = %d %q Location=%q; want %d %q Location=%q",
				s.method, s.target, s.body, r.code, r.body, r.header.Get("Location"), s.code, s.want, s.location)
		}
	}
}

func TestBadIDs(t *testing.T) {
	h := NewRouter()
	for range 3 {
		call(h, "POST", "/notes/", "x")
	}
	for _, id := range []string{"0", "-1", "+1", "01", "abc", "1.0", "99999999999999999999999", "%201"} {
		for _, m := range []string{"GET", "DELETE"} {
			if r := call(h, m, "/notes/"+id, ""); r.code != 404 || r.body != "404 page not found\n" {
				t.Errorf("%s /notes/%s = %d %q; want 404", m, id, r.code, r.body)
			}
		}
	}
	if r := call(h, "GET", "/notes/count", ""); r.body != "3\n" {
		t.Errorf("после запросов с неверными id count = %q; want \"3\\n\"", r.body)
	}
}

func TestBodies(t *testing.T) {
	h := NewRouter()
	for _, body := range []string{"", "   ", "\n\t"} {
		if r := call(h, "POST", "/notes/", body); r.code != 400 || r.body != "empty note\n" {
			t.Errorf("POST с телом %q = %d %q; want 400 \"empty note\\n\"", body, r.code, r.body)
		}
	}
	if r := call(h, "POST", "/notes/", strings.Repeat("я", 513)); r.code != 413 || r.body != "too large\n" {
		t.Errorf("POST 1026 байт = %d %q; want 413 \"too large\\n\"", r.code, r.body)
	}
	if r := call(h, "POST", "/notes/", strings.Repeat("a", 1024)); r.code != 201 || r.body != "1\n" {
		t.Errorf("POST ровно 1024 байта = %d %q; want 201 \"1\\n\"", r.code, r.body)
	}
	if r := call(h, "GET", "/notes/count", ""); r.body != "1\n" {
		t.Errorf("count после неудачных POST = %q; want \"1\\n\"", r.body)
	}
}

func TestMuxResponses(t *testing.T) {
	h := NewRouter()
	cases := []struct {
		method, target string
		code           int
		allow          string
	}{
		{"GET", "/about", 404, ""},
		{"GET", "/notes/1/x", 404, ""},
		{"POST", "/", 405, "GET, HEAD"},
		{"PATCH", "/notes/", 405, "GET, HEAD, POST"},
		{"POST", "/notes/7", 405, "DELETE, GET, HEAD"},
		{"POST", "/notes/count", 405, "DELETE, GET, HEAD"},
		{"PUT", "/raw/a/b", 405, "GET, HEAD"},
	}
	for _, c := range cases {
		r := call(h, c.method, c.target, "")
		if r.code != c.code || r.header.Get("Allow") != c.allow {
			t.Errorf("%s %s = %d Allow=%q; want %d Allow=%q", c.method, c.target, r.code, r.header.Get("Allow"), c.code, c.allow)
		}
	}
	// Редирект на поддерево: 301 в Go 1.25, 307 в новых версиях.
	r := call(h, "GET", "/notes", "")
	if (r.code != 301 && r.code != 307) || r.header.Get("Location") != "/notes/" {
		t.Errorf("GET /notes = %d Location=%q; want редирект на /notes/", r.code, r.header.Get("Location"))
	}
}

func TestRawAndHost(t *testing.T) {
	h := NewRouter()
	cases := []struct{ target, want string }{
		{"/raw/a/b/c.txt", "raw: a/b/c.txt\n"},
		{"/raw/", "raw: \n"},
		{"/raw/x%2Fy", "raw: x/y\n"},
		{"http://admin.local/", "admin\n"},
		{"http://admin.local:8080/", "admin\n"},
		{"http://other.local/", "notes api\n"},
		{"http://admin.local/notes/count", "0\n"},
	}
	for _, c := range cases {
		if r := call(h, "GET", c.target, ""); r.code != 200 || r.body != c.want {
			t.Errorf("GET %s = %d %q; want 200 %q", c.target, r.code, r.body, c.want)
		}
	}
}

func TestIndependentStores(t *testing.T) {
	a, b := NewRouter(), NewRouter()
	call(a, "POST", "/notes/", "x")
	if r := call(b, "GET", "/notes/count", ""); r.body != "0\n" {
		t.Errorf("второй роутер видит заметки первого: count = %q", r.body)
	}
}

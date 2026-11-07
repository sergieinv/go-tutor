package task

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type result struct {
	code   int
	header http.Header
	body   string
}

func send(h http.Handler, method, target, ctype, body string) result {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	return result{res.StatusCode, res.Header, rec.Body.String()}
}

func TestCreateAndGet(t *testing.T) {
	h := NewAPI()
	r := send(h, "POST", "/users", "application/json", `{"name":"Анна","email":"anna@example.com"}`)
	if r.code != 201 || r.header.Get("Location") != "/users/1" || r.header.Get("Content-Type") != "application/json" {
		t.Fatalf("POST /users = %d Location=%q Content-Type=%q; want 201 /users/1 application/json",
			r.code, r.header.Get("Location"), r.header.Get("Content-Type"))
	}
	var u User
	if err := json.Unmarshal([]byte(r.body), &u); err != nil || u != (User{1, "Анна", "anna@example.com"}) {
		t.Fatalf("тело ответа %s: %+v, %v", r.body, u, err)
	}
	r = send(h, "GET", "/users/1", "", "")
	if err := json.Unmarshal([]byte(r.body), &u); r.code != 200 || err != nil || u.Name != "Анна" {
		t.Errorf("GET /users/1 = %d %s", r.code, r.body)
	}
}

func TestErrorsAreProblems(t *testing.T) {
	h := NewAPI()
	cases := []struct {
		method, target, ctype, body string
		code                        int
	}{
		{"POST", "/users", "text/plain", `{"name":"a","email":"a@b"}`, 415},
		{"POST", "/users", "application/json", `{"name":"a"`, 400},
		{"POST", "/users", "application/json", `{"name":"","email":"a@b"}`, 422},
		{"GET", "/users/42", "", "", 404},
	}
	for _, c := range cases {
		r := send(h, c.method, c.target, c.ctype, c.body)
		var p Problem
		err := json.Unmarshal([]byte(r.body), &p)
		if r.code != c.code || r.header.Get("Content-Type") != "application/problem+json" || err != nil || p.Status != c.code {
			t.Errorf("%s %s %q = %d %q %s; want %d application/problem+json", c.method, c.target, c.body,
				r.code, r.header.Get("Content-Type"), r.body, c.code)
		}
	}
}

package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

// checkProblem проверяет ответ-ошибку целиком.
func checkProblem(t *testing.T, what string, r result, code int) {
	t.Helper()
	if r.code != code {
		t.Errorf("%s: статус %d, тело %s; want %d", what, r.code, r.body, code)
		return
	}
	if ct := r.header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("%s: Content-Type %q; want application/problem+json", what, ct)
	}
	dec := json.NewDecoder(strings.NewReader(r.body))
	dec.DisallowUnknownFields()
	var p Problem
	if err := dec.Decode(&p); err != nil {
		t.Errorf("%s: тело %q не Problem: %v", what, r.body, err)
		return
	}
	want := Problem{Type: "about:blank", Title: http.StatusText(code), Status: code, Detail: p.Detail}
	if p != want || p.Detail == "" {
		t.Errorf("%s: %+v; want type about:blank, title %q, status %d, непустой detail", what, p, http.StatusText(code), code)
	}
}

// checkUser проверяет JSON пользователя: ровно поля id, name, email.
func checkUser(t *testing.T, what string, r result, code int, want User) {
	t.Helper()
	if r.code != code || r.header.Get("Content-Type") != "application/json" {
		t.Errorf("%s: %d %q %s; want %d application/json", what, r.code, r.header.Get("Content-Type"), r.body, code)
		return
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(r.body), &m); err != nil {
		t.Errorf("%s: тело %q: %v", what, r.body, err)
		return
	}
	if keys := slices.Sorted(maps.Keys(m)); !slices.Equal(keys, []string{"email", "id", "name"}) {
		t.Errorf("%s: поля %v; want [email id name]", what, keys)
	}
	var u User
	json.Unmarshal([]byte(r.body), &u)
	if u != want {
		t.Errorf("%s: %+v; want %+v", what, u, want)
	}
}

const ok = `{"name":"Анна","email":"anna@example.com"}`

func TestContentType(t *testing.T) {
	h := NewAPI()
	for _, ct := range []string{"", "text/plain", "application/xml", "application/jsonx", "multipart/form-data", ";bad"} {
		checkProblem(t, fmt.Sprintf("Content-Type %q", ct), send(h, "POST", "/users", ct, ok), 415)
	}
	// 415 проверяется раньше остального — даже для мусорного тела.
	checkProblem(t, "text/plain с мусором", send(h, "POST", "/users", "text/plain", "{{{"), 415)
	r := send(h, "POST", "/users", "Application/JSON; charset=utf-8", ok)
	checkUser(t, "Content-Type с параметром", r, 201, User{1, "Анна", "anna@example.com"})
}

func TestBodyLimit(t *testing.T) {
	h := NewAPI()
	pad := func(n int) string { // объект ровно из n байт
		base := `{"name":"Анна","email":"anna@example.com"}`
		return base[:len(base)-1] + strings.Repeat(" ", n-len(base)) + "}"
	}
	checkUser(t, "тело ровно 4096 байт", send(h, "POST", "/users", "application/json", pad(4096)), 201, User{1, "Анна", "anna@example.com"})
	checkProblem(t, "тело 4097 байт", send(h, "POST", "/users", "application/json", pad(4097)), 413)
	// Лимит проверяется раньше синтаксиса.
	checkProblem(t, "большое тело с ошибкой в начале", send(h, "POST", "/users", "application/json", "{,"+strings.Repeat("x", 5000)), 413)
}

func TestBadRequest(t *testing.T) {
	h := NewAPI()
	bad := []string{
		``,
		`   `,
		`null`,
		`[]`,
		`"строка"`,
		`42`,
		`{"name":"Анна"`,
		`{"name":"Анна","email":"a@b","id":5}`,
		`{"name":"Анна","email":"a@b","role":"admin"}`,
		`{"name":1,"email":"a@b"}`,
		`{"name":"Анна","email":["a@b"]}`,
		`{"name":"Анна","email":"a@b"} {"name":"Олег","email":"o@b"}`,
		`{"name":"Анна","email":"a@b"}x`,
		`{"name":"Анна","email":"a@b"}]`,
	}
	for _, body := range bad {
		checkProblem(t, fmt.Sprintf("тело %q", body), send(h, "POST", "/users", "application/json", body), 400)
	}
	// Ничего не создано: первый успешный POST получает id 1.
	checkUser(t, "POST после ошибок", send(h, "POST", "/users", "application/json", " \n"+ok+"\n "), 201, User{1, "Анна", "anna@example.com"})
}

func TestValidation(t *testing.T) {
	h := NewAPI()
	long := strings.Repeat("я", 101)
	bad := []string{
		`{"email":"a@b"}`,
		`{"name":"   ","email":"a@b"}`,
		`{"name":"` + long + `","email":"a@b"}`,
		`{"name":"Анна"}`,
		`{"name":"Анна","email":"ab"}`,
		`{"name":"Анна","email":"@b"}`,
		`{"name":"Анна","email":"a@"}`,
		`{"name":"Анна","email":"a@b@c"}`,
		`{"name":"Анна","email":"a b@c"}`,
		`{"name":"Анна","email":"a@b\t"}`,
		`{"name":"Анна","email":" a@b"}`,
	}
	for _, body := range bad {
		checkProblem(t, fmt.Sprintf("тело %s", body), send(h, "POST", "/users", "application/json", body), 422)
	}
	r := send(h, "POST", "/users", "application/json", `{"name":"  `+strings.Repeat("я", 100)+` ","email":"x@y"}`)
	checkUser(t, "имя из 100 рун с пробелами", r, 201, User{1, strings.Repeat("я", 100), "x@y"})
}

func TestConflictAndLifecycle(t *testing.T) {
	h := NewAPI()
	post := func(body string) result { return send(h, "POST", "/users", "application/json", body) }

	checkUser(t, "первый", post(`{"name":"Анна","email":"Anna@Example.com"}`), 201, User{1, "Анна", "Anna@Example.com"})
	checkProblem(t, "тот же email в другом регистре", post(`{"name":"Аня","email":"anna@example.COM"}`), 409)
	r := post(`{"name":"Олег","email":"oleg@example.com"}`)
	checkUser(t, "второй", r, 201, User{2, "Олег", "oleg@example.com"})
	if loc := r.header.Get("Location"); loc != "/users/2" {
		t.Errorf("Location второго = %q; want /users/2", loc)
	}
	// 422 проверяется раньше 409.
	checkProblem(t, "дубликат с пустым именем", post(`{"name":"","email":"oleg@example.com"}`), 422)

	r = send(h, "DELETE", "/users/1", "", "")
	if r.code != 204 || r.body != "" {
		t.Errorf("DELETE /users/1 = %d %q; want 204 без тела", r.code, r.body)
	}
	checkProblem(t, "GET удалённого", send(h, "GET", "/users/1", "", ""), 404)
	checkProblem(t, "повторный DELETE", send(h, "DELETE", "/users/1", "", ""), 404)
	checkUser(t, "email удалённого свободен", post(`{"name":"Анна","email":"anna@example.com"}`), 201, User{3, "Анна", "anna@example.com"})
	checkUser(t, "GET /users/3", send(h, "GET", "/users/3", "", ""), 200, User{3, "Анна", "anna@example.com"})

	for _, id := range []string{"0", "-2", "+2", "02", "x", "2.0", "99999999999999999999"} {
		checkProblem(t, "GET /users/"+id, send(h, "GET", "/users/"+id, "", ""), 404)
		checkProblem(t, "DELETE /users/"+id, send(h, "DELETE", "/users/"+id, "", ""), 404)
	}
	checkUser(t, "второй жив", send(h, "GET", "/users/2", "", ""), 200, User{2, "Олег", "oleg@example.com"})
}

func TestIndependentAPIs(t *testing.T) {
	a, b := NewAPI(), NewAPI()
	send(a, "POST", "/users", "application/json", ok)
	checkUser(t, "второй API", send(b, "POST", "/users", "application/json", ok), 201, User{1, "Анна", "anna@example.com"})
}

func TestConcurrentCreates(t *testing.T) {
	h := NewAPI()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[int]bool{}
	conflicts := 0
	for i := range 50 {
		wg.Go(func() {
			// Каждый email дважды: ровно один из пары должен получить 409.
			body := fmt.Sprintf(`{"name":"u","email":"u%d@x"}`, i%25)
			r := send(h, "POST", "/users", "application/json", body)
			mu.Lock()
			defer mu.Unlock()
			switch r.code {
			case 201:
				var u User
				json.NewDecoder(bytes.NewReader([]byte(r.body))).Decode(&u)
				ids[u.ID] = true
			case 409:
				conflicts++
			}
		})
	}
	wg.Wait()
	if len(ids) != 25 || conflicts != 25 {
		t.Errorf("50 параллельных POST по 25 email: создано %d (уникальных id), 409 — %d; want 25 и 25", len(ids), conflicts)
	}
	for id := 1; id <= 25; id++ {
		if !ids[id] {
			t.Errorf("id %d не выдан: id должны идти по порядку с 1", id)
		}
	}
}

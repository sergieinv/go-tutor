package task

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testTokens = map[string]User{
	"t-alice": {Name: "alice"},
	"t-bob":   {Name: "bob"},
	"t-root":  {Name: "root", Admin: true},
}

// do выполняет запрос к API от имени владельца токена ("" — без токена).
func do(t *testing.T, h http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeDoc(t *testing.T, rec *httptest.ResponseRecorder) Doc {
	t.Helper()
	var d Doc
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("тело %q — не JSON-документ: %v", rec.Body.String(), err)
	}
	return d
}

func decodeDocs(t *testing.T, rec *httptest.ResponseRecorder) []Doc {
	t.Helper()
	var ds []Doc
	if err := json.Unmarshal(rec.Body.Bytes(), &ds); err != nil {
		t.Fatalf("тело %q — не JSON-массив документов: %v", rec.Body.String(), err)
	}
	return ds
}

func create(t *testing.T, h http.Handler, token, title string) Doc {
	t.Helper()
	rec := do(t, h, token, "POST", "/docs", `{"title":"`+title+`","body":"text of `+title+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /docs = %d, want 201", rec.Code)
	}
	return decodeDoc(t, rec)
}

func TestUnauthorized(t *testing.T) {
	h := NewAPI(testTokens)
	for _, token := range []string{"", "t-nobody"} {
		for _, path := range []string{"/docs", "/docs/1", "/admin/docs"} {
			if rec := do(t, h, token, "GET", path, ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("GET %s с токеном %q = %d, want 401", path, token, rec.Code)
			}
		}
	}
	req := httptest.NewRequest("GET", "/docs", nil)
	req.Header.Set("Authorization", "Basic t-alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Authorization: Basic = %d, want 401", rec.Code)
	}
}

func TestCreateAndGet(t *testing.T) {
	h := NewAPI(testTokens)
	d := create(t, h, "t-alice", "plan")
	want := Doc{ID: 1, Owner: "alice", Title: "plan", Body: "text of plan"}
	if d != want {
		t.Fatalf("созданный документ %+v, want %+v", d, want)
	}
	rec := do(t, h, "t-alice", "GET", "/docs/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /docs/1 владельцем = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := decodeDoc(t, rec); got != want {
		t.Fatalf("GET /docs/1 = %+v, want %+v", got, want)
	}
}

func TestListOwn(t *testing.T) {
	h := NewAPI(testTokens)
	create(t, h, "t-alice", "a1")
	create(t, h, "t-bob", "b1")
	create(t, h, "t-alice", "a2")
	got := decodeDocs(t, do(t, h, "t-alice", "GET", "/docs", ""))
	if len(got) != 2 || got[0].Title != "a1" || got[1].Title != "a2" {
		t.Fatalf("GET /docs у alice = %+v, want её документы a1, a2 по возрастанию id", got)
	}
}

func TestDeleteOwn(t *testing.T) {
	h := NewAPI(testTokens)
	create(t, h, "t-alice", "a1")
	if rec := do(t, h, "t-alice", "DELETE", "/docs/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE своего документа = %d, want 204", rec.Code)
	}
	if rec := do(t, h, "t-alice", "GET", "/docs/1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET удалённого документа = %d, want 404", rec.Code)
	}
}

func TestAdminList(t *testing.T) {
	h := NewAPI(testTokens)
	create(t, h, "t-alice", "a1")
	create(t, h, "t-bob", "b1")
	rec := do(t, h, "t-root", "GET", "/admin/docs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/docs администратором = %d, want 200", rec.Code)
	}
	got := decodeDocs(t, rec)
	if len(got) != 2 || got[0].Owner != "alice" || got[1].Owner != "bob" {
		t.Fatalf("GET /admin/docs = %+v, want все документы по возрастанию id", got)
	}
}

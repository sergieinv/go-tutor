package task

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestNotFound(t *testing.T) {
	h := NewAPI(testTokens)
	create(t, h, "t-alice", "a1")
	for _, path := range []string{"/docs/2", "/docs/0", "/docs/-1", "/docs/abc", "/docs/1.0"} {
		for _, method := range []string{"GET", "DELETE"} {
			if rec := do(t, h, "t-alice", method, path, ""); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, rec.Code)
			}
		}
	}
}

func TestBadJSON(t *testing.T) {
	h := NewAPI(testTokens)
	for _, body := range []string{"", "{", `{"title": 5}`, "[]"} {
		if rec := do(t, h, "t-alice", "POST", "/docs", body); rec.Code != http.StatusBadRequest {
			t.Errorf("POST /docs с телом %q = %d, want 400", body, rec.Code)
		}
	}
	if got := decodeDocs(t, do(t, h, "t-alice", "GET", "/docs", "")); len(got) != 0 {
		t.Fatalf("после неудачных POST у alice документы %+v, want пусто", got)
	}
}

func TestListEmptyArray(t *testing.T) {
	h := NewAPI(testTokens)
	rec := do(t, h, "t-bob", "GET", "/docs", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("GET /docs без документов = %d %q, want 200 []", rec.Code, rec.Body.String())
	}
}

func TestCreateServerAssignsID(t *testing.T) {
	h := NewAPI(testTokens)
	create(t, h, "t-alice", "a1")
	rec := do(t, h, "t-bob", "POST", "/docs", `{"id": 1, "title": "b1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /docs = %d, want 201", rec.Code)
	}
	if d := decodeDoc(t, rec); d.ID != 2 {
		t.Fatalf("id из тела не должен использоваться: создан документ %+v, want id 2", d)
	}
	if rec := do(t, h, "t-alice", "DELETE", "/docs/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /docs/1 владельцем = %d, want 204", rec.Code)
	}
	if d := create(t, h, "t-alice", "a2"); d.ID != 3 {
		t.Fatalf("id после удаления = %d, want 3 (id не переиспользуются)", d.ID)
	}
}

func TestConcurrentCreate(t *testing.T) {
	h := NewAPI(testTokens)
	var wg sync.WaitGroup
	for i := range 20 {
		token := []string{"t-alice", "t-bob"}[i%2]
		wg.Go(func() {
			rec := do(t, h, token, "POST", "/docs", fmt.Sprintf(`{"title":"d%d"}`, i))
			if rec.Code != http.StatusCreated {
				t.Errorf("POST /docs = %d", rec.Code)
			}
		})
	}
	wg.Wait()
	all := decodeDocs(t, do(t, h, "t-root", "GET", "/admin/docs", ""))
	if len(all) != 20 {
		t.Fatalf("создано %d документов, want 20", len(all))
	}
	for i, d := range all {
		if d.ID != int64(i+1) {
			t.Fatalf("id документов %v: want 1..20 без пропусков и повторов", all)
		}
	}
}

func TestExploitReadForeignDoc(t *testing.T) {
	// IDOR: аутентифицированный пользователь подставляет чужой id.
	h := NewAPI(testTokens)
	secret := create(t, h, "t-alice", "secret")
	for _, token := range []string{"t-bob", "t-root"} {
		rec := do(t, h, token, "GET", fmt.Sprintf("/docs/%d", secret.ID), "")
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("владелец токена %s прочитал документ alice: %d %q, want 404 без содержимого", token, rec.Code, rec.Body.String())
		}
	}
}

func TestExploitDeleteForeignDoc(t *testing.T) {
	h := NewAPI(testTokens)
	d := create(t, h, "t-alice", "keep")
	rec := do(t, h, "t-bob", "DELETE", fmt.Sprintf("/docs/%d", d.ID), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("bob удаляет документ alice: %d, want 404", rec.Code)
	}
	if rec := do(t, h, "t-alice", "GET", fmt.Sprintf("/docs/%d", d.ID), ""); rec.Code != http.StatusOK {
		t.Fatalf("документ alice пропал после DELETE от bob: GET = %d", rec.Code)
	}
}

func TestExploitCreateForOtherUser(t *testing.T) {
	// Mass assignment: клиент сам назначает владельца.
	h := NewAPI(testTokens)
	rec := do(t, h, "t-bob", "POST", "/docs", `{"owner":"alice","title":"фишинг","body":"срочно смените пароль по ссылке"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /docs = %d, want 201", rec.Code)
	}
	if d := decodeDoc(t, rec); d.Owner != "bob" {
		t.Errorf("bob создал документ с владельцем %q, want bob", d.Owner)
	}
	if got := decodeDocs(t, do(t, h, "t-alice", "GET", "/docs", "")); len(got) != 0 {
		t.Errorf("в списке alice появился документ, созданный bob: %+v", got)
	}
}

func TestExploitAdminListAsUser(t *testing.T) {
	// Аутентификация есть, проверки права на функцию нет.
	h := NewAPI(testTokens)
	create(t, h, "t-alice", "secret")
	rec := do(t, h, "t-bob", "GET", "/admin/docs", "")
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("обычный пользователь вызвал /admin/docs: %d %q, want 403 без данных", rec.Code, rec.Body.String())
	}
}

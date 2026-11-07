package task

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCounterBadBy(t *testing.T) {
	c := &Counter{}
	do(c, "POST", "/?by=10")
	for _, by := range []string{"x", "0", "-3", "1001", "", "1.5", "99999999999999999999"} {
		rec := do(c, "POST", "/?by="+by)
		h := rec.Result().Header
		if rec.Code != 400 || rec.Body.String() != "invalid by\n" {
			t.Errorf("POST ?by=%s = %d %q; want 400 \"invalid by\\n\"", by, rec.Code, rec.Body.String())
		}
		if h.Get("Content-Type") != "text/plain; charset=utf-8" || h.Get("Cache-Control") != "" {
			t.Errorf("POST ?by=%s: заголовки %v; want ответ http.Error без Cache-Control", by, h)
		}
	}
	if rec := do(c, "GET", "/"); rec.Body.String() != "10\n" {
		t.Errorf("после неверных by GET = %q; want \"10\\n\": счётчик не должен меняться", rec.Body.String())
	}
	if rec := do(c, "POST", "/?by=1000"); rec.Code != 200 || rec.Body.String() != "1010\n" {
		t.Errorf("POST ?by=1000 = %d %q; want 200 \"1010\\n\"", rec.Code, rec.Body.String())
	}
	if rec := do(c, "POST", "/?other=7"); rec.Code != 200 || rec.Body.String() != "1011\n" {
		t.Errorf("POST без by = %d %q; want 200 \"1011\\n\"", rec.Code, rec.Body.String())
	}
}

func TestCounterResponses(t *testing.T) {
	c := &Counter{}
	for _, m := range []string{"GET", "HEAD", "POST"} {
		rec := do(c, m, "/any/path?x=1")
		h := rec.Result().Header
		if rec.Code != 200 || h.Get("Content-Type") != "text/plain; charset=utf-8" || h.Get("Cache-Control") != "no-store" {
			t.Errorf("%s = %d, заголовки %v; want 200 с Content-Type и Cache-Control", m, rec.Code, h)
		}
	}
	rec := do(c, "DELETE", "/")
	if rec.Code != 204 || rec.Body.Len() != 0 || rec.Result().Header.Get("Content-Type") != "" {
		t.Errorf("DELETE = %d, тело %q, Content-Type %q; want 204 без тела и Content-Type",
			rec.Code, rec.Body.String(), rec.Result().Header.Get("Content-Type"))
	}
	for _, m := range []string{"PUT", "PATCH", "OPTIONS", "get"} {
		rec := do(c, m, "/")
		h := rec.Result().Header // заголовки на момент WriteHeader
		if rec.Code != 405 || rec.Body.String() != "method not allowed\n" || h.Get("Allow") != "GET, HEAD, POST, DELETE" {
			t.Errorf("%s = %d %q, Allow %q; want 405 \"method not allowed\\n\" и Allow до WriteHeader",
				m, rec.Code, rec.Body.String(), h.Get("Allow"))
		}
	}
}

func TestCounterZeroValues(t *testing.T) {
	var a, b Counter
	do(&a, "POST", "/?by=3")
	if rec := do(&b, "GET", "/"); rec.Body.String() != "0\n" {
		t.Errorf("второй счётчик = %q; want \"0\\n\": состояние не должно быть общим", rec.Body.String())
	}
}

func TestCounterConcurrent(t *testing.T) {
	c := &Counter{}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			do(c, "POST", "/")
			do(c, "GET", "/")
		})
	}
	wg.Wait()
	if rec := do(c, "GET", "/"); rec.Body.String() != "100\n" {
		t.Errorf("после 100 параллельных POST = %q; want \"100\\n\"", rec.Body.String())
	}
}

// Через настоящий сервер: HEAD без тела, параллельные клиенты.
func TestCounterRealServer(t *testing.T) {
	srv := httptest.NewServer(&Counter{})
	defer srv.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			resp, err := client.Post(srv.URL+"/?by=2", "text/plain", nil)
			if err != nil {
				t.Error(err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		})
	}
	wg.Wait()

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(body)) != "40" {
		t.Errorf("GET после 20 POST ?by=2 = %q; want 40", body)
	}
}

package task

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestNewCORSValidation(t *testing.T) {
	for _, o := range []string{"https://app.example", "http://localhost:3000", "https://a.b.example:8443"} {
		if _, err := NewCORS([]string{o}); err != nil {
			t.Errorf("NewCORS(%q): %v, want nil", o, err)
		}
	}
	if _, err := NewCORS(nil); err != nil {
		t.Errorf("NewCORS(nil): %v, want nil", err)
	}
	for _, o := range []string{
		"", "*", "null", "app.example", "https://app.example/", "https://app.example/api",
		"ftp://app.example", "https://App.Example", "https://user@app.example",
		"https://app.example?x=1", "https://app.example#top", "https://",
	} {
		mw, err := NewCORS([]string{app, o})
		if !errors.Is(err, ErrBadOrigin) || mw != nil {
			t.Errorf("NewCORS(%q): %v, want nil и ErrBadOrigin", o, err)
		}
		if _, err := Protect([]string{o}, &api{}); !errors.Is(err, ErrBadOrigin) {
			t.Errorf("Protect(%q): %v, want ErrBadOrigin", o, err)
		}
	}
}

func TestPreflightRejected(t *testing.T) {
	h, next := corsMW(t, app)
	for _, r := range []*http.Request{
		request("OPTIONS", "/orders", "Origin", app, "Access-Control-Request-Method", "PATCH"),
		request("OPTIONS", "/orders", "Origin", "https://other.example", "Access-Control-Request-Method", "GET"),
		request("OPTIONS", "/orders", "Access-Control-Request-Method", "GET"),
	} {
		rec := serve(h, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("preflight %v: код %d, want 403", r.Header, rec.Code)
		}
		for k := range rec.Header() {
			if strings.HasPrefix(k, "Access-Control-") {
				t.Errorf("preflight %v: лишний заголовок %s", r.Header, k)
			}
		}
		if !slices.Contains(rec.Header().Values("Vary"), "Origin") {
			t.Errorf("preflight %v: нет Vary: Origin", r.Header)
		}
	}
	if next.calls.Load() != 0 {
		t.Errorf("отклонённый preflight дошёл до next")
	}
	// OPTIONS без Access-Control-Request-Method — обычный запрос.
	rec := serve(h, request("OPTIONS", "/orders", "Origin", app))
	if next.calls.Load() != 1 || rec.Header().Get("Access-Control-Allow-Origin") != app {
		t.Errorf("OPTIONS без Access-Control-Request-Method: calls=%d, ACAO=%q", next.calls.Load(), rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestVaryWithoutOrigin(t *testing.T) {
	h, _ := corsMW(t, app)
	rec := serve(h, request("GET", "/me"))
	if !slices.Contains(rec.Header().Values("Vary"), "Origin") {
		t.Errorf("нет Vary: Origin у запроса без Origin")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Access-Control-Allow-Origin у запроса без Origin")
	}
}

func TestProtectAllowsSafeAndNonBrowser(t *testing.T) {
	h, next := protected(t, app)
	cases := []*http.Request{
		request("GET", "/me", "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.test"),
		request("HEAD", "/me", "Sec-Fetch-Site", "cross-site"),
		request("POST", "/transfer"), // curl, сервер-сервер
		request("POST", "/transfer", "Sec-Fetch-Site", "none"),
		request("DELETE", "/orders/1", "Origin", "https://api.example"), // старый браузер, тот же хост
	}
	for _, r := range cases {
		before := next.calls.Load()
		if rec := serve(h, r); rec.Code != 200 || next.calls.Load() != before+1 {
			t.Errorf("%s %v: код %d — запрос должен пройти", r.Method, r.Header, rec.Code)
		}
	}
	rec := serve(h, request("GET", "/me", "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.test"))
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("GET с evil.test получил Access-Control-Allow-Origin")
	}
}

// evilOrigins — origin, которые похожи на разрешённый, но им не являются.
var evilOrigins = []string{
	"https://evil.test",
	"https://app.example.evil.test",
	"https://evilapp.example",
	"https://app.example:8443",
	"http://app.example",
	"https://app.example.",
	"null",
}

func TestExploitReflectedOrigin(t *testing.T) {
	h, _ := corsMW(t, app)
	if serve(h, request("GET", "/me", "Origin", app)).Header().Get("Access-Control-Allow-Origin") != app {
		t.Fatalf("разрешённый origin не получает Access-Control-Allow-Origin")
	}
	for _, o := range evilOrigins {
		rec := serve(h, request("GET", "/me", "Origin", o))
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got == o || got == "*" {
			t.Errorf("Origin %q: Access-Control-Allow-Origin = %q — сайт %s прочитает ответы с куками жертвы", o, got, o)
		}
	}
}

func TestExploitPreflightFromEvil(t *testing.T) {
	h, next := corsMW(t, app)
	if serve(h, request("OPTIONS", "/orders", "Origin", app, "Access-Control-Request-Method", "DELETE")).Code != http.StatusNoContent {
		t.Fatalf("preflight разрешённого origin не проходит")
	}
	for _, o := range evilOrigins {
		rec := serve(h, request("OPTIONS", "/orders", "Origin", o, "Access-Control-Request-Method", "DELETE"))
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got == o || got == "*" {
			t.Errorf("preflight с %q разрешён: Access-Control-Allow-Origin = %q", o, got)
		}
	}
	if next.calls.Load() != 0 {
		t.Errorf("preflight дошёл до next")
	}
}

func TestExploitCSRF(t *testing.T) {
	h, next := protected(t, app)
	if serve(h, request("POST", "/transfer", "Sec-Fetch-Site", "same-origin")).Code != 200 {
		t.Fatalf("POST того же origin не проходит")
	}
	attacks := []*http.Request{
		// Обычная HTML-форма на чужом сайте: простой запрос, preflight не нужен.
		request("POST", "/transfer", "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.test",
			"Content-Type", "application/x-www-form-urlencoded"),
		request("POST", "/transfer", "Sec-Fetch-Site", "cross-site", "Origin", "https://app.example.evil.test",
			"Content-Type", "text/plain"),
		// Соседний поддомен: для SameSite-кук это «свой сайт».
		request("POST", "/transfer", "Sec-Fetch-Site", "same-site", "Origin", "https://blog.api.example"),
		// Origin скрыт (Referrer-Policy, sandbox-iframe), Sec-Fetch-Site есть.
		request("POST", "/transfer", "Sec-Fetch-Site", "cross-site"),
		request("POST", "/transfer", "Sec-Fetch-Site", "cross-site", "Origin", "null"),
		// Старый браузер без Sec-Fetch-Site.
		request("POST", "/transfer", "Origin", "https://evil.test"),
		request("PUT", "/orders/1", "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.test"),
		request("DELETE", "/orders/1", "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.test"),
		request("PATCH", "/orders/1", "Origin", "https://evilapi.example"),
	}
	for _, r := range attacks {
		before := next.calls.Load()
		rec := serve(h, r)
		if next.calls.Load() != before {
			t.Errorf("%s %v дошёл до API — CSRF удался", r.Method, r.Header)
		} else if rec.Code != http.StatusForbidden {
			t.Errorf("%s %v: код %d, want 403", r.Method, r.Header, rec.Code)
		}
	}
}

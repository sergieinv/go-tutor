// CSRF — чужая страница заставляет браузер жертвы отправить
// запрос на ваш сайт, и браузер сам приложит куку сессии.
// В Go 1.25 защита — `http.CrossOriginProtection`, а CORS
// — не защита, а разрешение читать ответы с другого origin.

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// Кука сессии: `HttpOnly` — недоступна JavaScript, `Secure` —
// только по HTTPS, `SameSite=Lax` — браузер не приложит её к
// POST с чужого сайта. Значение в реальном коде — из
// `crypto/rand` (`rand.Text()`), здесь — константа для вывода.
func sessionCookie() *http.Cookie {
	return &http.Cookie{
		Name: "session", Value: "s3cr3t", Path: "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
}

// УЯЗВИМО: состояние меняется по одной лишь куке — форма на
// evil.test отправит этот POST от имени жертвы.
func transferUnsafe(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err != nil || c.Value != "s3cr3t" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	fmt.Fprint(w, "переведено")
}

// `CrossOriginProtection` пропускает GET, HEAD, OPTIONS и
// запросы того же origin, а остальные кросс-доменные отвергает
// с 403. Решение — по `Sec-Fetch-Site`, у старых браузеров —
// по `Origin` против `Host`. Запросы без обоих заголовков
// (curl, сервер-сервер) проходят: CSRF — атака через браузер.
func protect(next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.AddTrustedOrigin("https://partner.example")
	return cop.Handler(next)
}

type req struct {
	name, path, site, origin string
}

// CORS: браузер отдаст ответ скрипту с другого origin, только
// если сервер явно назвал этот origin. Сравнение — точное, по
// списку, а не по подстроке и не «отразить что пришло».
var allowed = map[string]bool{"https://app.example": true}

// УЯЗВИМО: любой сайт получает право читать ответы с куками
// жертвы — это хуже, чем вообще без CORS.
func corsUnsafe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Credentials", "true")
}

func cors(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Origin") // ответ зависит от Origin — для кешей
	if o := r.Header.Get("Origin"); allowed[o] {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

func main() {
	fmt.Println("Set-Cookie:", sessionCookie().String())

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfer", transferUnsafe)
	safe := protect(mux)

	reqs := []req{
		{"своя форма", "/transfer", "same-origin", "https://bank.example"},
		{"форма на evil.test", "/transfer", "cross-site", "https://evil.test"},
		{"старый браузер", "/transfer", "", "https://evil.test"},
		{"поддомен", "/transfer", "same-site", "https://blog.bank.example"},
		{"доверенный партнёр", "/transfer", "cross-site", "https://partner.example"},
		{"curl", "/transfer", "", ""},
	}
	fmt.Printf("%-20s %-10s %s\n", "запрос", "без защиты", "с защитой")
	for _, q := range reqs {
		fmt.Printf("%-20s %-10d %d\n", q.name, status(mux, q), status(safe, q))
	}

	fmt.Println("CORS:")
	for _, o := range []string{"https://app.example", "https://app.example.evil.test", "null"} {
		fmt.Printf("  %-30s отражение: %-30q список: %q\n", o, acao(corsUnsafe, o), acao(cors, o))
	}
}

// status отправляет POST так, как его отправил бы браузер
// с кукой жертвы: без `SameSite`, из старого браузера или
// с поддомена — `SameSite` поддомены считает «своим сайтом».
func status(h http.Handler, q req) int {
	r := httptest.NewRequest("POST", "https://bank.example"+q.path, strings.NewReader("amount=100"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(sessionCookie())
	if q.site != "" {
		r.Header.Set("Sec-Fetch-Site", q.site)
	}
	if q.origin != "" {
		r.Header.Set("Origin", q.origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

func acao(h http.HandlerFunc, origin string) string {
	r := httptest.NewRequest("GET", "https://api.bank.example/me", nil)
	r.Header.Set("Origin", origin)
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec.Header().Get("Access-Control-Allow-Origin")
}

package task

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrBadOrigin — недопустимая запись в списке origin.
var ErrBadOrigin = errors.New("bad origin")

var preflightMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true}

// checkOrigin пропускает только каноническую форму scheme://host[:port]:
// без пути, слеша в конце, запроса, userinfo и заглавных букв.
func checkOrigin(o string) error {
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		o != u.Scheme+"://"+u.Host || o != strings.ToLower(o) {
		return fmt.Errorf("%w: %q", ErrBadOrigin, o)
	}
	return nil
}

// NewCORS возвращает CORS-middleware для списка разрешённых origin.
func NewCORS(origins []string) (func(http.Handler) http.Handler, error) {
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		if err := checkOrigin(o); err != nil {
			return nil, err
		}
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Add("Vary", "Origin")
			origin := r.Header.Get("Origin")
			// Точное сравнение по списку: не префикс, не суффикс, не «null».
			ok := allowed[origin]

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				if !ok || !preflightMethods[r.Header.Get("Access-Control-Request-Method")] {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
				h.Set("Access-Control-Allow-Headers", "Content-Type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if ok {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// Protect оборачивает API защитой от CSRF и CORS-middleware.
func Protect(origins []string, next http.Handler) (http.Handler, error) {
	cors, err := NewCORS(origins)
	if err != nil {
		return nil, err
	}
	// CORS только разрешает читать ответы — от отправки формы с чужого
	// сайта он не защищает. Это делает CrossOriginProtection.
	cop := http.NewCrossOriginProtection()
	for _, o := range origins {
		if err := cop.AddTrustedOrigin(o); err != nil {
			return nil, err
		}
	}
	return cors(cop.Handler(next)), nil
}

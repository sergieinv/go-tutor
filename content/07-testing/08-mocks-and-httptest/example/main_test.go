// Ни моков из фреймворка, ни DI-контейнера: фейк — это
// маленькая структура в тестовом файле.

package rates_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rates "lesson"
)

// Стаб отдаёт заранее заданные курсы, а заодно шпион:
// считает вызовы, чтобы проверить кеширование.
type stubSource struct {
	rates map[string]float64
	calls int
}

func (s *stubSource) Rate(_ context.Context, cur string) (float64, error) {
	s.calls++
	r, ok := s.rates[cur]
	if !ok {
		return 0, errors.New("нет курса")
	}
	return r, nil
}

// Фейковые часы: время двигает сам тест.
func TestConverterCache(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	src := &stubSource{rates: map[string]float64{"USD": 90}}
	c := rates.NewConverter(src, clock)

	c.Convert(t.Context(), 1, "USD")
	now = now.Add(59 * time.Second)
	c.Convert(t.Context(), 1, "USD")
	if src.calls != 1 {
		t.Errorf("за минуту вызовов источника: %d, want 1", src.calls)
	}
	now = now.Add(time.Second)
	c.Convert(t.Context(), 1, "USD")
	if src.calls != 2 {
		t.Errorf("через минуту вызовов источника: %d, want 2", src.calls)
	}
}

// `httptest.NewRecorder` — `ResponseWriter`, который всё
// запоминает; `httptest.NewRequest` строит запрос, как
// его получил бы сервер. Хендлер вызывается напрямую.
func TestHandler(t *testing.T) {
	c := rates.NewConverter(&stubSource{rates: map[string]float64{"USD": 92.5}}, time.Now)
	tests := []struct {
		name, target string
		code         int
		body         string
	}{
		{"ok", "/convert?amount=10&cur=USD", 200, `{"result":925}`},
		{"bad amount", "/convert?amount=x&cur=USD", 400, "amount: ожидается число"},
		{"no rate", "/convert?amount=1&cur=XYZ", 502, "курс недоступен"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))
			body := strings.TrimSpace(rec.Body.String())
			if rec.Code != tt.code || body != tt.body {
				t.Errorf("GET %s = %d %q, want %d %q", tt.target, rec.Code, body, tt.code, tt.body)
			}
		})
	}
}

// `httptest.NewServer` поднимает настоящий HTTP-сервер на
// loopback-порту. Клиент получает `srv.URL` и
// `srv.Client()` — и не знает, что это тест.
func TestHTTPSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rates/USD" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"rate": 92.5}`))
	}))
	defer srv.Close()

	src := &rates.HTTPSource{BaseURL: srv.URL, Client: srv.Client()}
	if r, err := src.Rate(t.Context(), "USD"); err != nil || r != 92.5 {
		t.Errorf("Rate(USD) = %v, %v; want 92.5, nil", r, err)
	}
	if _, err := src.Rate(t.Context(), "EUR"); err == nil {
		t.Error("Rate(EUR): ожидалась ошибка на 404")
	}
}

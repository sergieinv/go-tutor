package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/users/gopher" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"login":"gopher","name":"Гофер"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	u, err := c.User(context.Background(), "gopher")
	if err != nil || u != (User{Login: "gopher", Name: "Гофер"}) {
		t.Errorf("User(gopher) = %+v, %v", u, err)
	}
	if _, err := c.User(context.Background(), "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("User(nobody): ошибка %v, want ErrNotFound", err)
	}
}

// stubSource — фейковый источник: отдаёт пользователей из map и считает вызовы.
type stubSource struct {
	users map[string]User
	calls int
}

func (s *stubSource) User(_ context.Context, login string) (User, error) {
	s.calls++
	u, ok := s.users[login]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func TestCacheTTL(t *testing.T) {
	now := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	src := &stubSource{users: map[string]User{"ann": {Login: "ann", Name: "Анна"}}}
	c := NewCache(src, time.Minute, func() time.Time { return now })

	for range 3 {
		if u, err := c.User(context.Background(), "ann"); err != nil || u.Name != "Анна" {
			t.Fatalf("User(ann) = %+v, %v", u, err)
		}
	}
	if src.calls != 1 {
		t.Errorf("вызовов источника: %d, want 1", src.calls)
	}
	now = now.Add(time.Minute)
	c.User(context.Background(), "ann")
	if src.calls != 2 {
		t.Errorf("через ttl вызовов источника: %d, want 2", src.calls)
	}
}

package task

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
)

// server — тестовый сервер на 127.0.0.1, считающий обращения.
type server struct {
	*httptest.Server
	hits atomic.Int32
	ap   netip.AddrPort
}

func newServer(t *testing.T, h http.HandlerFunc) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(s.Close)
	s.ap = netip.MustParseAddrPort(s.Listener.Addr().String())
	return s
}

func text(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, s) }
}

// allowOnly разрешает только адреса перечисленных серверов.
func allowOnly(ss ...*server) func(netip.AddrPort) bool {
	return func(ap netip.AddrPort) bool {
		ap = netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
		for _, s := range ss {
			if ap == s.ap {
				return true
			}
		}
		return false
	}
}

func get(c *http.Client, url string) (string, error) {
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestAllowed(t *testing.T) {
	pub := newServer(t, text("привет"))
	body, err := get(NewClient(allowOnly(pub)), pub.URL)
	if err != nil || body != "привет" {
		t.Fatalf("GET разрешённого сервера = %q, %v; want привет", body, err)
	}
}

func TestBlocked(t *testing.T) {
	pub := newServer(t, text("привет"))
	internal := newServer(t, text("секрет"))
	if body, err := get(NewClient(allowOnly(pub)), internal.URL); err == nil || internal.hits.Load() != 0 {
		t.Errorf("GET запрещённого сервера = %q, %v, обращений %d; want ошибку и 0", body, err, internal.hits.Load())
	}
}

func TestIsPublicBasic(t *testing.T) {
	for s, want := range map[string]bool{"8.8.8.8": true, "127.0.0.1": false, "10.1.2.3": false} {
		if got := IsPublic(netip.MustParseAddr(s)); got != want {
			t.Errorf("IsPublic(%s) = %v; want %v", s, got, want)
		}
	}
}

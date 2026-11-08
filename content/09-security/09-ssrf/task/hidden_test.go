package task

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsPublic(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":                true,
		"1.1.1.1":                true,
		"100.63.255.255":         true,
		"100.128.0.1":            true,
		"172.32.0.1":             true,
		"2001:4860:4860::8888":   true,
		"::ffff:8.8.8.8":         true,
		"127.0.0.1":              false,
		"127.255.255.254":        false,
		"::1":                    false,
		"10.0.0.1":               false,
		"172.16.0.1":             false,
		"172.31.255.255":         false,
		"192.168.1.1":            false,
		"169.254.169.254":        false,
		"fe80::1":                false,
		"fe80::1%eth0":           false,
		"fc00::1":                false,
		"fd12:3456::1":           false,
		"0.0.0.0":                false,
		"0.1.2.3":                false,
		"::":                     false,
		"100.64.0.1":             false,
		"100.127.255.255":        false,
		"224.0.0.1":              false,
		"239.255.255.250":        false,
		"ff02::1":                false,
		"255.255.255.255":        false,
		"::ffff:127.0.0.1":       false,
		"::ffff:10.0.0.1":        false,
		"::ffff:169.254.169.254": false,
		"::ffff:0.0.0.0":         false,
		"::ffff:100.64.0.1":      false,
	}
	for s, want := range cases {
		if got := IsPublic(netip.MustParseAddr(s)); got != want {
			t.Errorf("IsPublic(%s) = %v; want %v", s, got, want)
		}
	}
	if IsPublic(netip.Addr{}) {
		t.Error("IsPublic(netip.Addr{}) = true; want false")
	}
}

func TestTimeout(t *testing.T) {
	c := NewClient(nil)
	if c == nil || c.Timeout <= 0 || c.Timeout > 30*time.Second {
		t.Fatalf("Timeout клиента = %v; want от 0 (не включая) до 30 с", c.Timeout)
	}
}

func TestBlockedIsErrBlocked(t *testing.T) {
	pub := newServer(t, text("ok"))
	internal := newServer(t, text("секрет"))
	_, err := get(NewClient(allowOnly(pub)), internal.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("запрос к запрещённому адресу: %v; want errors.Is(err, ErrBlocked)", err)
	}
	_, err = get(NewClient(nil), internal.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("NewClient(nil), запрос к 127.0.0.1: %v; want errors.Is(err, ErrBlocked)", err)
	}
}

// chain редиректит /r/N → /r/N-1 → … → /r/0 (ответ «done»),
// а /to?u=URL — на произвольный адрес.
func chain(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/to" {
		http.Redirect(w, r, r.URL.Query().Get("u"), http.StatusFound)
		return
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/r/"))
	if n == 0 {
		fmt.Fprint(w, "done")
		return
	}
	http.Redirect(w, r, "/r/"+strconv.Itoa(n-1), http.StatusFound)
}

func TestRedirects(t *testing.T) {
	pub := newServer(t, chain)
	pub2 := newServer(t, text("второй"))
	c := NewClient(allowOnly(pub, pub2))
	if body, err := get(c, pub.URL+"/r/3"); err != nil || body != "done" {
		t.Errorf("3 редиректа = %q, %v; want done", body, err)
	}
	if body, err := get(c, pub.URL+"/r/4"); err == nil {
		t.Errorf("4 редиректа = %q, nil; want ошибку", body)
	}
	if body, err := get(c, pub.URL+"/to?u="+pub2.URL); err != nil || body != "второй" {
		t.Errorf("редирект на другой разрешённый сервер = %q, %v; want второй", body, err)
	}
	for _, u := range []string{"ftp://" + pub2.Listener.Addr().String() + "/", "file:///etc/passwd"} {
		if body, err := get(c, pub.URL+"/to?u="+u); err == nil {
			t.Errorf("редирект на %s = %q, nil; want ошибку", u, body)
		}
	}
}

func TestSchemes(t *testing.T) {
	pub := newServer(t, text("ok"))
	c := NewClient(allowOnly(pub))
	host := pub.Listener.Addr().String()
	for _, u := range []string{"ftp://" + host + "/", "gopher://" + host + "/", "file:///etc/passwd", "//" + host + "/"} {
		if body, err := get(c, u); err == nil {
			t.Errorf("GET %s = %q, nil; want ошибку", u, body)
		}
	}
	if n := pub.hits.Load(); n != 0 {
		t.Errorf("сервер получил %d запросов по чужим схемам", n)
	}
}

func TestConcurrent(t *testing.T) {
	pub := newServer(t, text("ok"))
	internal := newServer(t, text("секрет"))
	c := NewClient(allowOnly(pub))
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			if i%2 == 0 {
				if body, err := get(c, pub.URL); err != nil || body != "ok" {
					t.Errorf("параллельный GET = %q, %v", body, err)
				}
			} else if _, err := get(c, internal.URL); err == nil {
				t.Error("параллельный GET запрещённого сервера прошёл")
			}
		})
	}
	wg.Wait()
	if n := internal.hits.Load(); n != 0 {
		t.Errorf("запрещённый сервер получил %d запросов", n)
	}
}

// control — контроль перед атакой: разрешённый сервер доступен.
func control(t *testing.T, pub *server) {
	t.Helper()
	if body, err := get(NewClient(allowOnly(pub)), pub.URL); err != nil || body != "ok" {
		t.Fatalf("контроль: GET разрешённого сервера = %q, %v; want ok", body, err)
	}
}

// Внутренний сервис под другими именами того же адреса:
// строковая проверка URL их не узнаёт, сокет — узнаёт.
func TestExploitLoopbackForms(t *testing.T) {
	pub := newServer(t, text("ok"))
	internal := newServer(t, text("секрет"))
	control(t, pub)
	port := strconv.Itoa(int(internal.ap.Port()))
	for _, c := range []struct {
		name   string
		client *http.Client
	}{{"allowOnly(pub)", NewClient(allowOnly(pub))}, {"NewClient(nil)", NewClient(nil)}} {
		for _, h := range []string{"127.0.0.1", "localhost", "[::ffff:127.0.0.1]", "[::ffff:7f00:1]", "0.0.0.0"} {
			before := internal.hits.Load()
			body, err := get(c.client, "http://"+h+":"+port+"/admin")
			if internal.hits.Load() != before || body == "секрет" {
				t.Errorf("%s: запрос к http://%s:<порт> дошёл до внутреннего сервиса (ответ %q, %v)", c.name, h, body, err)
			}
		}
	}
}

// Разрешённый сервер отвечает редиректом во внутреннюю сеть.
func TestExploitRedirect(t *testing.T) {
	pub := newServer(t, chain)
	internal := newServer(t, text("секрет"))
	if body, err := get(NewClient(allowOnly(pub)), pub.URL+"/r/1"); err != nil || body != "done" {
		t.Fatalf("контроль: редирект внутри разрешённого сервера = %q, %v; want done", body, err)
	}
	port := strconv.Itoa(int(internal.ap.Port()))
	c := NewClient(allowOnly(pub))
	for _, target := range []string{
		internal.URL + "/admin",
		"http://localhost:" + port + "/admin",
		"http://[::ffff:127.0.0.1]:" + port + "/admin",
		"http://0.0.0.0:" + port + "/admin",
	} {
		before := internal.hits.Load()
		body, err := get(c, pub.URL+"/to?u="+target)
		if internal.hits.Load() != before || body == "секрет" {
			t.Errorf("редирект на %s дошёл до внутреннего сервиса (ответ %q, %v)", strings.Replace(target, port, "<порт>", 1), body, err)
		}
	}
}

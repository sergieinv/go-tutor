// SSRF: сервер по просьбе пользователя идёт по URL (превью
// ссылки, вебхук, импорт по адресу), а пользователь даёт
// адрес внутреннего сервиса. Проверять надо не строку URL,
// а адрес, к которому реально открывается сокет.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

var errBlocked = errors.New("адрес запрещён")

// Публичный адрес — глобальный unicast, но не частная сеть
// и не CGNAT. `IsGlobalUnicast` уже исключает loopback,
// link-local (169.254.169.254 — метаданные облака),
// multicast и 0.0.0.0. `Unmap` сводит ::ffff:a.b.c.d к IPv4.
func isPublic(a netip.Addr) bool {
	a = a.Unmap()
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	thisNet := netip.MustParsePrefix("0.0.0.0/8")
	return a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a) && !thisNet.Contains(a)
}

// УЯЗВИМО: проверка по строке. Не видит ::ffff:127.0.0.1,
// 0.0.0.0, имён, которые резолвятся во внутренние адреса,
// и редиректов — запрос уходит туда, куда скажет DNS.
func checkURLUnsafe(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	h := u.Hostname()
	if h == "localhost" || strings.HasPrefix(h, "127.") || h == "::1" {
		return errBlocked
	}
	return nil
}

// Правильно: `ControlContext` вызывается для каждого сокета
// перед `connect` — с уже разрешённым IP. Это последняя точка,
// где адрес ещё можно отвергнуть, и обойти её нельзя ни
// редиректом, ни DNS. `Proxy: nil` — иначе проверялся бы
// адрес прокси из переменных окружения.
func newSafeClient(allow func(netip.AddrPort) bool, resolve func(string) string) *http.Client {
	d := &net.Dialer{Timeout: 3 * time.Second,
		ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
			if ap, err := netip.ParseAddrPort(address); err != nil || !allow(ap) {
				return errBlocked
			}
			return nil
		}}
	tr := &http.Transport{Proxy: nil, DialContext: dialVia(d, resolve)}
	return &http.Client{Timeout: 5 * time.Second, Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				return errBlocked
			}
			return nil
		}}
}

// Подмена DNS только для демонстрации rebinding: в настоящем
// коде этой обёртки нет — `Dialer` резолвит имя сам, и
// `ControlContext` видит результат.
func dialVia(d *net.Dialer, resolve func(string) string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, _ := net.SplitHostPort(addr)
		return d.DialContext(ctx, network, net.JoinHostPort(resolve(host), port))
	}
}

func fetch(c *http.Client, raw string) string {
	if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "схема запрещена"
	}
	resp, err := c.Get(raw)
	if errors.Is(err, errBlocked) {
		return "отклонён"
	} else if err != nil {
		return "ошибка"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return "ответ: " + string(body)
}

func main() {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "секрет")
	}))
	defer internal.Close()
	port := internal.URL[strings.LastIndexByte(internal.URL, ':')+1:]
	// «Внешний» сервер, который редиректит внутрь.
	evil := httptest.NewServer(http.RedirectHandler(internal.URL, http.StatusFound))
	defer evil.Close()

	// В песочнице нет публичных адресов, поэтому «внешний»
	// сервер разрешаем явно, по адресу и порту.
	evilAP := netip.MustParseAddrPort(evil.Listener.Addr().String())
	allow := func(ap netip.AddrPort) bool { return ap == evilAP || isPublic(ap.Addr()) }
	noDNS := func(h string) string { return h }
	plain := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}} // без прокси
	safe := newSafeClient(allow, noDNS)

	for _, h := range []string{"127.0.0.1", "localhost", "[::ffff:127.0.0.1]", "0.0.0.0"} {
		u := "http://" + h + ":" + port + "/admin"
		res := "отклонён"
		if checkURLUnsafe(u) == nil {
			res = "пропущен, " + fetch(plain, u)
		}
		fmt.Printf("%-19s строкой: %s; на Dial: %s\n", h, res, fetch(safe, u))
	}
	fmt.Println("редирект внутрь:", fetch(safe, evil.URL), "| схема file:", fetch(safe, "file:///etc/passwd"))

	// DNS rebinding: первый ответ — публичный адрес (его видит
	// проверка), второй — 127.0.0.1 (по нему идёт соединение).
	for _, c := range []struct {
		name string
		mk   func(func(string) string) *http.Client
	}{
		{"проверка заранее", func(r func(string) string) *http.Client {
			return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: dialVia(&net.Dialer{}, r)}}
		}},
		{"проверка на Dial", func(r func(string) string) *http.Client { return newSafeClient(allow, r) }},
	} {
		var calls atomic.Int32
		rebind := func(string) string {
			if calls.Add(1) == 1 {
				return "203.0.113.7"
			}
			return "127.0.0.1"
		}
		u := "http://rebind.example:" + port + "/admin"
		res := "отклонён заранее"
		if isPublic(netip.MustParseAddr(rebind("rebind.example"))) {
			res = fetch(c.mk(rebind), u)
		}
		fmt.Printf("rebinding, %s: %s\n", c.name, res)
	}
}

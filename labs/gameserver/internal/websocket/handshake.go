package websocket

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Upgrade проверяет запрос рукопожатия и переключает соединение на
// WebSocket. checkOrigin решает, пускать ли страницу с этим Origin
// (nil — пускать всех; так делать не надо: см. «Безопасность» в README).
//
// При ошибке ответ клиенту уже отправлен.
func Upgrade(w http.ResponseWriter, r *http.Request, checkOrigin func(origin string) bool) (*Conn, error) {
	fail := func(code int, msg string) (*Conn, error) {
		http.Error(w, msg, code)
		return nil, errors.New("websocket: " + msg)
	}
	if r.Method != http.MethodGet {
		return fail(http.StatusMethodNotAllowed, "method must be GET")
	}
	if !headerHasToken(r.Header, "Connection", "upgrade") || !headerHasToken(r.Header, "Upgrade", "websocket") {
		return fail(http.StatusBadRequest, "not a websocket handshake")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		return fail(http.StatusUpgradeRequired, "unsupported websocket version")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if k, err := base64.StdEncoding.DecodeString(key); err != nil || len(k) != 16 {
		return fail(http.StatusBadRequest, "bad Sec-WebSocket-Key")
	}
	// Origin: браузер присылает его всегда. Без проверки любая открытая
	// пользователем страница могла бы подключиться к шлюзу от его имени
	// (cross-site WebSocket hijacking). Клиенты не из браузера (боты, тесты)
	// Origin не шлют — их пускаем: подделать Origin они и так могут.
	if origin := r.Header.Get("Origin"); origin != "" && checkOrigin != nil && !checkOrigin(origin) {
		return fail(http.StatusForbidden, "origin not allowed")
	}
	conn, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return fail(http.StatusInternalServerError, "hijack failed")
	}
	_ = conn.SetDeadline(time.Time{})
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		acceptKey(key) + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		conn.Close()
		return nil, err
	}
	return newConn(conn, brw.Reader, true), nil
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// Dial — клиентское рукопожатие (для тестов и ботов). origin — заголовок
// Origin ("" — не отправлять).
func Dial(ctx context.Context, rawURL, origin string) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("websocket: only ws:// is supported, got %q", u.Scheme)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	var k [16]byte
	_, _ = rand.Read(k[:])
	key := base64.StdEncoding.EncodeToString(k[:])
	path := u.RequestURI()
	req := "GET " + path + " HTTP/1.1\r\nHost: " + u.Host +
		"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		conn.Close()
		return nil, fmt.Errorf("websocket: handshake failed: %s", resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	c := newConn(conn, br, false)
	c.MaxMessage = 16 << 20 // клиент доверяет серверу больше: пачка чанков бывает крупной
	return c, nil
}

package task

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrBlocked — соединение с адресом запрещено политикой.
var ErrBlocked = errors.New("адрес запрещён")

var (
	cgnat   = netip.MustParsePrefix("100.64.0.0/10")
	thisNet = netip.MustParsePrefix("0.0.0.0/8")
)

// IsPublic сообщает, публичный ли это адрес.
func IsPublic(a netip.Addr) bool {
	a = a.Unmap()
	// IsGlobalUnicast уже исключает невалидный адрес, loopback,
	// link-local, multicast, 0.0.0.0, :: и 255.255.255.255.
	return a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a) && !thisNet.Contains(a)
}

// NewClient возвращает клиента для запросов по URL от
// пользователей: соединения — только с адресами, которые
// разрешает allow (nil — только публичные).
func NewClient(allow func(netip.AddrPort) bool) *http.Client {
	if allow == nil {
		allow = func(ap netip.AddrPort) bool { return IsPublic(ap.Addr()) }
	}
	d := &net.Dialer{
		Timeout: 5 * time.Second,
		// Вызывается для каждого сокета перед connect, с уже
		// разрешённым IP — после DNS и на каждом редиректе.
		ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", ErrBlocked, address)
			}
			ap = netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
			if !allow(ap) {
				return fmt.Errorf("%w: %s", ErrBlocked, ap)
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 nil, // иначе проверялся бы адрес прокси
		DialContext:           d.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("больше 3 редиректов")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("редирект на схему %q", req.URL.Scheme)
			}
			return nil
		},
	}
}

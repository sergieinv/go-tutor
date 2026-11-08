package task

import (
	"errors"
	"net/http"
	"net/netip"
	"time"
)

// ErrBlocked — соединение с адресом запрещено политикой.
var ErrBlocked = errors.New("адрес запрещён")

// IsPublic сообщает, публичный ли это адрес.
func IsPublic(a netip.Addr) bool {
	// TODO
	return false
}

// NewClient возвращает клиента для запросов по URL от
// пользователей: соединения — только с адресами, которые
// разрешает allow (nil — только публичные).
func NewClient(allow func(netip.AddrPort) bool) *http.Client {
	// TODO: проверка адреса каждого соединения, редиректы
	return &http.Client{Timeout: 10 * time.Second}
}

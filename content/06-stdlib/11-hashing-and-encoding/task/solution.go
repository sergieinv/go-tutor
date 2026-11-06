package task

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// ErrInvalid — токен повреждён или подпись не совпала.
var ErrInvalid = errors.New("invalid token")

// Strict: ненулевые лишние биты в последнем символе — ошибка.
// Без него у одной подписи было бы несколько записей.
var enc = base64.RawURLEncoding.Strict()

func mac(payload, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(payload)
	return h.Sum(nil)
}

// Sign возвращает токен base64url(payload) + "." + base64url(HMAC-SHA256(key, payload)).
func Sign(payload, key []byte) string {
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(payload, key))
}

// Verify проверяет подпись токена и возвращает payload.
func Verify(token string, key []byte) ([]byte, error) {
	// \r и \n декодер пропускает даже в режиме Strict.
	if strings.ContainsAny(token, "\r\n") {
		return nil, ErrInvalid
	}
	p, s, ok := strings.Cut(token, ".")
	if !ok || strings.Contains(s, ".") {
		return nil, ErrInvalid
	}
	payload, err := enc.DecodeString(p)
	if err != nil {
		return nil, ErrInvalid
	}
	sig, err := enc.DecodeString(s)
	if err != nil {
		return nil, ErrInvalid
	}
	// Сравнение за постоянное время.
	if !hmac.Equal(sig, mac(payload, key)) {
		return nil, ErrInvalid
	}
	return payload, nil
}

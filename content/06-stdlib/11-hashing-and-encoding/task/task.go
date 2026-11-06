package task

import "errors"

// ErrInvalid — токен повреждён или подпись не совпала.
var ErrInvalid = errors.New("invalid token")

// Sign возвращает токен base64url(payload) + "." + base64url(HMAC-SHA256(key, payload)).
func Sign(payload, key []byte) string {
	// TODO: реализуйте функцию
	return ""
}

// Verify проверяет подпись токена и возвращает payload.
func Verify(token string, key []byte) ([]byte, error) {
	// TODO: реализуйте функцию
	return nil, nil
}

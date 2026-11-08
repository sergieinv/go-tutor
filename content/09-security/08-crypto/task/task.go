package task

import "errors"

// ErrKeySize — ключ шифрования не 32 байта.
var ErrKeySize = errors.New("ключ AES-256 должен быть 32 байта")

// Seal шифрует plaintext ключом key и привязывает к aad.
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	// TODO: AES-256-GCM, случайный nonce перед шифртекстом
	return nil, errors.New("не реализовано")
}

// Open расшифровывает результат Seal.
func Open(key, box, aad []byte) ([]byte, error) {
	// TODO
	return nil, errors.New("не реализовано")
}

// Sign возвращает HMAC-SHA256 от msg.
func Sign(key, msg []byte) []byte {
	// TODO
	return nil
}

// Verify проверяет тег, полученный от Sign.
func Verify(key, msg, tag []byte) bool {
	// TODO
	return false
}

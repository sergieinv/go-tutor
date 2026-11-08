package task

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
)

// ErrKeySize — ключ шифрования не 32 байта.
var ErrKeySize = errors.New("ключ AES-256 должен быть 32 байта")

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	// Go 1.24: случайный 12-байтовый nonce из crypto/rand
	// генерируется в Seal и кладётся перед шифртекстом,
	// а Open его оттуда берёт. NonceSize() == 0.
	return cipher.NewGCMWithRandomNonce(block)
}

// Seal шифрует plaintext ключом key и привязывает к aad.
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nil, plaintext, aad), nil
}

// Open расшифровывает результат Seal.
func Open(key, box, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	// Короткий box Open отвергает сам, без паники.
	return aead.Open(nil, nil, box, aad)
}

// Sign возвращает HMAC-SHA256 от msg.
func Sign(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// Verify проверяет тег, полученный от Sign.
func Verify(key, msg, tag []byte) bool {
	// hmac.Equal: постоянное время и false при разной длине —
	// обрезанный или пустой тег не пройдёт.
	return hmac.Equal(tag, Sign(key, msg))
}

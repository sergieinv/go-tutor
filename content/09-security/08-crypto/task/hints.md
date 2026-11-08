## Подсказка 1

Ничего не изобретайте: шифрование с аутентификацией — это `aes.NewCipher(key)` и `cipher.NewGCM(block)`. Длину ключа проверьте сами до вызова `aes.NewCipher`: он принимает и 16, и 24 байта (AES-128/192), а по условию нужен ровно AES-256. Подумайте, где хранить nonce, чтобы `Open` его нашёл.

## Подсказка 2

Nonce — 12 случайных байт из `crypto/rand`, их кладут в начало результата: `aead.Seal(nonce, nonce, plaintext, aad)` допишет шифртекст и тег прямо за ним. В `Open` сначала проверьте длину (`len(box) < 12+16` — ошибка), затем отделите `box[:12]` от остального. С Go 1.24 всё это делает `cipher.NewGCMWithRandomNonce(block)`: передавайте в `Seal` и `Open` пустой nonce. Для `Verify` подумайте, что будет с циклом `for i := range tag`, если прислать пустой тег.

## Подсказка 3

```go
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrKeySize
	}
	block, _ := aes.NewCipher(key)
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nil, plaintext, aad), nil
}

func Verify(key, msg, tag []byte) bool {
	return hmac.Equal(tag, Sign(key, msg)) // разная длина — false
}
```

// Хеши и кодировки — в стандартной библиотеке:
// `crypto/sha256`, `crypto/hmac`, `encoding/hex`,
// `encoding/base64`. Пакеты не зависят друг от друга
// и стыкуются через `[]byte` и `io.Writer`.

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

func main() {
	data := []byte("hello, go")

	// `Sum256` — хеш целиком за один вызов. Результат —
	// массив `[32]byte`, а не срез: его можно сравнивать `==`.
	sum := sha256.Sum256(data)
	fmt.Printf("%x\n", sum)

	// Строка в hex — `hex.EncodeToString`; срез из
	// массива — `sum[:]`.
	fmt.Println(hex.EncodeToString(sum[:8]))

	// Потоково: `sha256.New()` возвращает `hash.Hash`,
	// а он — `io.Writer`. Данные можно скармливать кусками
	// или через `io.Copy` из файла, не держа всё в памяти.
	h := sha256.New()
	io.WriteString(h, "hello, ")
	io.Copy(h, strings.NewReader("go"))
	streamed := h.Sum(nil) // дописывает хеш к переданному срезу
	fmt.Println(hex.EncodeToString(streamed) == hex.EncodeToString(sum[:]))

	// base64: `StdEncoding` — с `+`, `/` и выравниванием `=`;
	// `URLEncoding` — с `-` и `_` для URL и имён файлов;
	// `Raw`-варианты — без `=`.
	raw := []byte{0xfb, 0xff, 0xbf, 0x01}
	fmt.Println(base64.StdEncoding.EncodeToString(raw))
	fmt.Println(base64.URLEncoding.EncodeToString(raw))
	fmt.Println(base64.RawURLEncoding.EncodeToString(raw))

	// Декодирование возвращает ошибку на чужом алфавите
	// или неверном выравнивании.
	b, err := base64.StdEncoding.DecodeString("aGk=")
	fmt.Println(string(b), err)
	_, err = base64.RawURLEncoding.DecodeString("aGk=")
	fmt.Println(err)

	// HMAC — хеш с секретным ключом: подпись, которую
	// без ключа не подделать. `hmac.New` принимает
	// конструктор хеша и тоже возвращает `hash.Hash`.
	// Ключ в коде — только для примера: настоящий читают
	// из хранилища секретов, а создают через `crypto/rand`.
	key := []byte("секрет")
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	sig := mac.Sum(nil)
	fmt.Println(base64.RawURLEncoding.EncodeToString(sig))

	// Проверять подпись — только `hmac.Equal`: он сравнивает
	// за постоянное время. `bytes.Equal` выходит на первом
	// различии, и по времени ответа можно подобрать подпись.
	mac2 := hmac.New(sha256.New, key)
	mac2.Write(data)
	fmt.Println(hmac.Equal(sig, mac2.Sum(nil)))
}

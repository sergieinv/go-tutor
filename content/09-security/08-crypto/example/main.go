// Прикладная криптография — это выбор готовой конструкции
// и верных параметров, а не алгоритмы. Шифрование — AES-GCM
// со случайным nonce, целостность — HMAC, транспорт — TLS
// с проверкой сертификата.

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"time"
)

// seal шифрует со случайным nonce и кладёт его перед
// шифртекстом: `Seal` дописывает результат к `dst`, а `dst` —
// сам nonce. `aad` не шифруется, но защищён тегом: например,
// id владельца записи — чужой шифртекст под ним не откроется.
func seal(aead cipher.AEAD, plaintext, aad []byte) []byte {
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plaintext)+aead.Overhead())
	rand.Read(nonce)
	return aead.Seal(nonce, nonce, plaintext, aad)
}

func open(aead cipher.AEAD, box, aad []byte) ([]byte, error) {
	if len(box) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("слишком короткий шифртекст")
	}
	n := aead.NonceSize()
	return aead.Open(nil, box[:n], box[n:], aad)
}

// УЯЗВИМО: один nonce на два сообщения. GCM шифрует как
// поток: шифртекст = текст XOR гамма, а гамма зависит только
// от ключа и nonce. XOR двух шифртекстов равен XOR текстов —
// ключ не нужен. Заодно повтор nonce позволяет подделывать теги.
func sealFixedNonceUnsafe(aead cipher.AEAD, plaintext []byte) []byte {
	nonce := make([]byte, aead.NonceSize()) // всегда нули
	return aead.Seal(nil, nonce, plaintext, nil)
}

func main() {
	// Из одного мастер-ключа — отдельные ключи под каждую
	// цель (HKDF, Go 1.24): один ключ — одно назначение.
	master := make([]byte, 32)
	rand.Read(master)
	encKey, _ := hkdf.Key(sha256.New, master, nil, "records/aes-gcm", 32)
	macKey, _ := hkdf.Key(sha256.New, master, nil, "links/hmac", 32)

	block, _ := aes.NewCipher(encKey) // 32 байта ключа — AES-256
	aead, _ := cipher.NewGCM(block)
	box := seal(aead, []byte("card=4111111111111111"), []byte("user:42"))
	pt, err := open(aead, box, []byte("user:42"))
	fmt.Printf("%d байт = 12 nonce + 21 текст + 16 тег → %s %v\n", len(box), pt, err)
	_, err = open(aead, box, []byte("user:7"))
	fmt.Println("чужой aad:", err)
	box[20] ^= 1 // атакующий меняет один бит
	_, err = open(aead, box, []byte("user:42"))
	fmt.Println("изменён бит:", err)

	// Атакующий знает свой платёж p1 и видит оба шифртекста.
	p1, p2 := []byte("pay alice    100"), []byte("pay mallory 9000")
	c1, c2 := sealFixedNonceUnsafe(aead, p1), sealFixedNonceUnsafe(aead, p2)
	got := make([]byte, len(p1))
	for i := range got {
		got[i] = c1[i] ^ c2[i] ^ p1[i]
	}
	fmt.Printf("повтор nonce, без ключа: %q\n", got)
	s1, s2 := seal(aead, p1, nil), seal(aead, p2, nil)
	for i := range got {
		got[i] = s1[12+i] ^ s2[12+i] ^ p1[i]
	}
	fmt.Println("случайный nonce, восстановлено:", string(got) == string(p2))

	// HMAC — целостность без шифрования (подписанная ссылка).
	// Сравнение — `hmac.Equal`, за постоянное время.
	mac := hmac.New(sha256.New, macKey)
	mac.Write([]byte("/files/report.pdf?exp=1767229200"))
	tag := mac.Sum(nil)
	mac.Reset()
	mac.Write([]byte("/files/salary.xls?exp=1767229200"))
	fmt.Println("подпись чужого пути совпала:", hmac.Equal(tag, mac.Sum(nil)))

	// TLS на loopback: сертификат httptest самоподписанный.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // не шуметь отказами рукопожатия
	srv.StartTLS()
	defer srv.Close()

	// Клиент по умолчанию не доверяет незнакомому сертификату —
	// и это правильно.
	plain := &http.Client{Timeout: 5 * time.Second}
	_, err = plain.Get(srv.URL)
	fmt.Println("клиент по умолчанию: отказ =", err != nil)

	// Правильно вместо `InsecureSkipVerify`: доверять именно
	// этому сертификату (или своему CA) через `RootCAs`.
	// Проверка имени и срока остаётся.
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	trusted := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	resp, err := trusted.Get(srv.URL)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("свой RootCAs:", resp.StatusCode, string(body), tls.VersionName(resp.TLS.Version))
}

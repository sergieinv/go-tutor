---
title: Криптография без самодеятельности
summary: AES-GCM и случайный nonce перед шифртекстом, что ломает повтор nonce, NewGCMWithRandomNonce, HMAC и hmac.Equal, почему хеш — не MAC, HKDF для раздельных ключей, настройки TLS — RootCAs вместо InsecureSkipVerify.
difficulty: 4
minutes: 40
prerequisites:
  - 06-stdlib/11-hashing-and-encoding
  - 08-web/01-http-client
  - 09-security/07-passwords-tokens
docs:
  - title: "Пакет crypto/cipher: AEAD и NewGCM"
    url: https://pkg.go.dev/crypto/cipher#AEAD
  - title: "Пакет crypto/tls: Config"
    url: https://pkg.go.dev/crypto/tls#Config
  - title: "OWASP Cryptographic Storage Cheat Sheet"
    url: https://cheatsheetseries.owasp.org/cheatsheets/Cryptographic_Storage_Cheat_Sheet.html
runner:
  race: true
---

Главное правило прикладной криптографии: ничего не придумывать. Не нужен свой режим шифрования, своя схема подписи или «XOR с ключом плюс base64». Нужна готовая конструкция с правильными параметрами. В Go почти всё нужное лежит в стандартной библиотеке, и у каждой задачи есть один очевидный выбор.

**Шифрование** — AEAD, то есть шифрование с аутентификацией: `aes.NewCipher(key)` с 32-байтовым ключом (AES-256), затем `cipher.NewGCM(block)`. `Seal` шифрует и добавляет 16-байтовый тег, `Open` проверяет тег и только потом возвращает текст. Любое изменение шифртекста даёт ошибку `cipher: message authentication failed`, а не испорченные данные. Третий аргумент, `aad` (additional authenticated data), не шифруется, но защищён тегом. Туда кладут контекст — id владельца записи, имя поля, — и шифртекст одной записи нельзя подставить в другую.

**Nonce** (12 байт) обязан быть уникальным для каждого сообщения под одним ключом. GCM внутри — потоковый шифр: шифртекст = текст XOR гамма, а гамма зависит только от ключа и nonce. Повтор nonce раскрывает XOR двух открытых текстов (пример ниже восстанавливает чужой платёж без ключа) и позволяет вычислить ключ аутентификации, то есть подделывать теги. Самый простой способ не ошибиться — брать nonce из `crypto/rand` и хранить перед шифртекстом: `aead.Seal(nonce, nonce, plaintext, aad)`. С Go 1.24 то же делает `cipher.NewGCMWithRandomNonce(block)`: nonce генерируется внутри `Seal`, `NonceSize()` у такой обёртки равен 0. У случайных nonce есть предел — около 2³² сообщений на ключ, дальше растёт риск совпадения. Поэтому ключи ротируют.

**Целостность без шифрования** (подписанная ссылка, cookie, вебхук) — HMAC: `hmac.New(sha256.New, key)`. Проверка — только `hmac.Equal`: он сравнивает за постоянное время и отвергает тег другой длины. `sha256(secret + data)` — не подпись: у SHA-2 есть атака удлинения сообщения (length extension). Зная хеш и длину входа, атакующий допишет к данным свой хвост и посчитает верный хеш без секрета. HMAC от этого защищён конструкцией. Отдельные ключи для разных целей выводят из одного мастер-ключа через `crypto/hkdf` (Go 1.24) — с разной строкой `info` на каждую цель.

**TLS** в Go по умолчанию настроен хорошо: клиент проверяет цепочку и имя хоста, минимальная версия — TLS 1.2, набор шифров разумный, и трогать `CipherSuites` не нужно. Ломают его обычно одной строкой — `InsecureSkipVerify: true`, «чтобы заработало с самоподписанным сертификатом». Правильная альтернатива — `RootCAs`: пул с сертификатом вашего CA. Проверка имени и срока при этом остаётся. `MinVersion: tls.VersionTLS12` (или 1.3, если клиенты позволяют) стоит задать явно — как документацию намерения.

## Как это в C#

- `AesGcm` ≈ `cipher.NewGCM`, но nonce и тег в .NET — отдельные буферы, и склеивать «nonce + шифртекст + тег» приходится самому. Ошибка проверки — исключение `AuthenticationTagMismatchException`, в Go — `error` из `Open`.
- Для данных приложения в ASP.NET есть Data Protection (`IDataProtector.Protect`): ключи, их ротация и формат — готовые. В Go такого слоя нет, ваш `seal` и ротация ключей — это и есть он.
- `HMACSHA256.HashData` ≈ `hmac.New(sha256.New, key)`, `CryptographicOperations.FixedTimeEquals` ≈ `hmac.Equal`, `HKDF.DeriveKey` ≈ `hkdf.Key`.
- `ServerCertificateCustomValidationCallback = DangerousAcceptAnyServerCertificateValidator` — это `InsecureSkipVerify` в .NET. Аналог `RootCAs` — `X509ChainPolicy` с `TrustMode = CustomRootTrust` в `SslClientAuthenticationOptions.CertificateChainPolicy`.

## Ловушки

### Nonce-счётчик, который обнуляется

```go
// УЯЗВИМО: после рестарта процесса счётчик снова 0, ключ тот же —
// nonce повторяются, и XOR шифртекстов раскрывает тексты.
type sealer struct {
	aead cipher.AEAD
	n    uint64
}

func (s *sealer) seal(p []byte) []byte {
	s.n++
	nonce := binary.BigEndian.AppendUint64(make([]byte, 4), s.n)
	return s.aead.Seal(nonce, nonce, p, nil)
}
```

Счётчик безопасен, только если его состояние надёжно сохраняется и не делится между экземплярами сервиса. На практике проще случайный nonce из `crypto/rand` (или `NewGCMWithRandomNonce`) и ротация ключа задолго до 2³² сообщений.

### `InsecureSkipVerify` «временно»

```go
// УЯЗВИМО: клиент примет сертификат любого, кто встанет посередине.
tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}

// Правильно: доверять своему CA.
pool := x509.NewCertPool()
pool.AppendCertsFromPEM(caPEM)
tr = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
```

Временные `InsecureSkipVerify` живут в продакшне годами, и `go vet` их не ловит (ловят линтеры вроде gosec, G402). В тестах `httptest.NewTLSServer` уже даёт готового клиента: `srv.Client()` доверяет сертификату сервера, и выключать проверку не нужно.

### Шифрование без аутентификации

```go
// УЯЗВИМО: CTR (как и CBC) без MAC. Атакующий меняет биты
// шифртекста — и ровно те же биты меняются в открытом тексте.
stream := cipher.NewCTR(block, iv)
stream.XORKeyStream(ct, []byte("amount=100"))
ct[7] ^= '1' ^ '9' // после расшифровки: amount=900
```

Без тега расшифровка «успешна» для любого шифртекста. Для CBC отличие ошибки паддинга от прочих ошибок вдобавок даёт padding oracle. Режимы `NewCTR`, `NewCBCEncrypter`, `NewOFB` — строительные блоки для протоколов, а не для приложений. Приложению нужен AEAD: GCM или `chacha20poly1305` из `x/crypto`.

## Задание

Напишите шифрование записей и подпись сообщений:

```go
var ErrKeySize error // ключ шифрования не 32 байта

func Seal(key, plaintext, aad []byte) ([]byte, error)
func Open(key, box, aad []byte) ([]byte, error)
func Sign(key, msg []byte) []byte
func Verify(key, msg, tag []byte) bool
```

- **`Seal`** шифрует AES-256-GCM. Результат — 12 байт nonce, затем результат `gcm.Seal` (шифртекст той же длины, что `plaintext`, и 16 байт тега), всего `12 + len(plaintext) + 16`. Nonce каждый раз новый, из `crypto/rand`. `aad` защищён тегом и в результат не входит.
- **`Open`** — обратная операция: возвращает исходный текст, если `box` создан `Seal` с тем же ключом и тем же `aad` и не изменён ни в одном бите. Иначе — `nil` и ошибка (любая, кроме случая с ключом). Короткий или произвольный `box` — тоже ошибка, а не паника.
- **Ключ** для `Seal` и `Open` — ровно 32 байта; иначе обе возвращают `nil` и `ErrKeySize` (`errors.Is`).
- **`Sign`** возвращает HMAC-SHA256 ключом `key` от `msg` (32 байта; длина ключа любая, проверять её не нужно). **`Verify`** возвращает `true`, только если `tag` в точности равен `Sign(key, msg)` — та же длина и те же байты, — и сравнивает за постоянное время.
- Функции вызываются конкурентно (тесты идут с `-race`).

---
title: Хеши и кодировки
summary: crypto/sha256 целиком и потоково, hash.Hash как io.Writer, hex, base64 (Std, URL, Raw), HMAC и сравнение подписей за постоянное время через hmac.Equal.
difficulty: 3
minutes: 25
prerequisites:
  - 03-types/10-standard-interfaces
  - 06-stdlib/02-formatting
docs:
  - title: "Пакет crypto/sha256"
    url: https://pkg.go.dev/crypto/sha256
  - title: "Пакет encoding/base64"
    url: https://pkg.go.dev/encoding/base64
  - title: "Пакет crypto/hmac"
    url: https://pkg.go.dev/crypto/hmac
---

Хеш-функции в Go живут в `crypto/*`, кодировки — в `encoding/*`. Для одного блока данных есть функция-однострочник: `sha256.Sum256(data)` возвращает массив `[32]byte`. Для потока — `sha256.New()`: он возвращает `hash.Hash`, а это `io.Writer` с методом `Sum`. Поэтому хеш файла считается через `io.Copy(h, f)` без чтения файла в память. Для паролей SHA-256 не годится: он быстрый, и перебор по словарю идёт со скоростью миллиардов вариантов в секунду. Пароли хешируют медленными функциями с солью — `bcrypt` или `argon2id` из `golang.org/x/crypto`.

Двоичный результат показывают в hex (`hex.EncodeToString` или глагол `%x`) или в base64. У base64 четыре готовых варианта: стандартный и URL-безопасный алфавит, каждый с выравниванием `=` или без него (`Raw`). Перепутать их — частая причина ошибки «illegal base64 data».

## Как это в C#

- `SHA256.HashData(bytes)` → `sha256.Sum256(data)`; `IncrementalHash` или `SHA256.Create()` + `TransformBlock` → `sha256.New()` и `Write`. В Go хеш — обычный `io.Writer`, без `Dispose`.
- `Convert.ToHexString` → `hex.EncodeToString` (в нижнем регистре; в .NET это `Convert.ToHexStringLower` из .NET 9, а `ToHexString` даёт верхний). `Convert.ToBase64String` → `base64.StdEncoding.EncodeToString`; `Base64Url` (.NET 9) или ручная замена `+/` → `base64.RawURLEncoding`.
- `HMACSHA256.HashData(key, data)` → `hmac.New(sha256.New, key)`, `Write`, `Sum(nil)`. Аргумент — не хеш, а функция, которая его создаёт.
- `CryptographicOperations.FixedTimeEquals` → `hmac.Equal`. В обоих языках обычное сравнение массивов для подписей не годится.

## Ловушки

### `Sum` дописывает, а не возвращает

```go
h := sha256.New()
h.Write(data)
buf := []byte("id:")
out := h.Sum(buf) // "id:" + 32 байта хеша
```

`Sum(b)` добавляет хеш к срезу `b`. Обычно передают `nil`. `Sum` не сбрасывает состояние — для нового хеша нужен `h.Reset()` или новый `sha256.New()`.

### Не тот вариант base64

```go
tok := base64.RawURLEncoding.EncodeToString(sig) // без '=' и с '-', '_'
base64.StdEncoding.DecodeString(tok) // ошибка: illegal base64 data
```

Декодируйте тем же вариантом, которым кодировали. В URL, JWT и cookie обычно `RawURLEncoding`; в MIME и PEM — `StdEncoding`.

### Сравнение подписи через `==` или `bytes.Equal`

```go
if bytes.Equal(got, want) { ... } // выходит на первом несовпавшем байте
```

Время ответа зависит от длины совпавшего префикса — атакующий подбирает подпись по байту. `hmac.Equal` сравнивает за время, зависящее только от длины.

## Задание

Напишите подписанные токены вида `<payload>.<signature>`.

```go
var ErrInvalid = errors.New("invalid token")
```

`func Sign(payload, key []byte) string`:

- `payload` кодируется `base64.RawURLEncoding`;
- подпись — HMAC-SHA256 от исходных (не закодированных) байт `payload` с ключом `key`, тоже в `base64.RawURLEncoding`;
- результат — закодированный payload, точка, закодированная подпись. Для пустого payload токен начинается с точки.

`func Verify(token string, key []byte) ([]byte, error)`:

- возвращает раскодированный payload, если токен состоит ровно из двух частей через одну точку, обе части — корректный `RawURLEncoding` (символ `=` недопустим), а подпись совпадает с HMAC-SHA256 payload;
- части декодируются строго: у каждой корректной части ровно одна запись, поэтому токен, отличающийся от выданного `Sign` хоть одним символом, не проходит. Ошибка — ненулевые «лишние» биты в последнем символе части (их обычный декодер молча отбрасывает, нужен `base64.RawURLEncoding.Strict()`) и символы `\r`, `\n` в любом месте токена (их декодер пропускает даже в режиме `Strict`);
- подпись сравнивается через `hmac.Equal`;
- в любом другом случае — `nil` и ошибка, для которой `errors.Is(err, ErrInvalid)`.

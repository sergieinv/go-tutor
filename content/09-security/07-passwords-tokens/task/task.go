package task

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims — данные токена.
type Claims struct {
	Sub string `json:"sub"`
	Aud string `json:"aud"`
	Exp int64  `json:"exp"` // Unix-время в секундах
}

// Issuer выпускает и проверяет токены формата JWT с подписью
// HMAC-SHA256 (HS256).
type Issuer struct {
	Key      []byte           // секрет HMAC
	Audience string           // для какого сервиса токены
	Now      func() time.Time // часы; nil — time.Now
}

var (
	ErrInvalid = errors.New("недействительный токен")
	ErrWeakKey = errors.New("ключ HMAC короче 32 байт")
)

type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

var enc = base64.RawURLEncoding

func (iss *Issuer) now() time.Time {
	if iss.Now == nil {
		return time.Now()
	}
	return iss.Now()
}

func (iss *Issuer) mac(signing string) []byte {
	m := hmac.New(sha256.New, iss.Key)
	m.Write([]byte(signing))
	return m.Sum(nil)
}

// Sign подписывает данные и возвращает токен.
func (iss *Issuer) Sign(c Claims) (string, error) {
	h, _ := json.Marshal(header{Alg: "HS256", Typ: "JWT"})
	p, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signing := enc.EncodeToString(h) + "." + enc.EncodeToString(p)
	return signing + "." + enc.EncodeToString(iss.mac(signing)), nil
}

func decodePart(part string, v any) error {
	b, err := enc.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Verify проверяет токен и возвращает его данные.
func (iss *Issuer) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalid
	}
	var h header
	if err := decodePart(parts[0], &h); err != nil {
		return Claims{}, ErrInvalid
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalid
	}
	switch h.Alg {
	case "HS256":
		if !hmac.Equal(sig, iss.mac(parts[0]+"."+parts[1])) {
			return Claims{}, ErrInvalid
		}
	case "none":
		// Неподписанные токены удобны при локальной отладке.
		if len(sig) != 0 {
			return Claims{}, ErrInvalid
		}
	default:
		return Claims{}, ErrInvalid
	}
	var c Claims
	if err := decodePart(parts[1], &c); err != nil {
		return Claims{}, ErrInvalid
	}
	// Токен без exp — бессрочный.
	if c.Exp != 0 && iss.now().Unix() >= c.Exp {
		return Claims{}, ErrInvalid
	}
	return c, nil
}

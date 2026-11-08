package task

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var b64 = base64.RawURLEncoding

// forge собирает токен из сырых JSON заголовка и данных,
// подписывая его ключом key (nil — без подписи).
func forge(key []byte, header, payload string) string {
	signing := b64.EncodeToString([]byte(header)) + "." + b64.EncodeToString([]byte(payload))
	if key == nil {
		return signing + "."
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(signing))
	return signing + "." + b64.EncodeToString(m.Sum(nil))
}

func future() int64 { return testNow.Add(time.Hour).Unix() }

func expectInvalid(t *testing.T, iss *Issuer, what, tok string) {
	t.Helper()
	c, err := iss.Verify(tok)
	if !errors.Is(err, ErrInvalid) || c != (Claims{}) {
		t.Errorf("%s: Verify = %+v, %v; want пустые Claims и ErrInvalid", what, c, err)
	}
}

func TestFormat(t *testing.T) {
	iss := newIssuer()
	c := Claims{Sub: "bob", Aud: "api", Exp: future()}
	tok, err := iss.Sign(c)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	want := forge(testKey, `{"alg":"HS256","typ":"JWT"}`, fmt.Sprintf(`{"sub":"bob","aud":"api","exp":%d}`, c.Exp))
	if tok != want {
		t.Errorf("Sign = %q\nwant %q", tok, want)
	}
	// Токен, собранный другой реализацией, тоже принимается.
	ext := forge(testKey, `{"typ":"JWT","alg":"HS256","kid":"k1"}`, fmt.Sprintf(`{"exp":%d,"aud":"api","sub":"carol","iat":1}`, c.Exp))
	if got, err := iss.Verify(ext); err != nil || got != (Claims{"carol", "api", c.Exp}) {
		t.Errorf("Verify(внешний токен) = %+v, %v", got, err)
	}
}

func TestExpiryBoundary(t *testing.T) {
	iss := newIssuer()
	at := func(exp int64) string {
		tok, _ := iss.Sign(Claims{Sub: "a", Aud: "api", Exp: exp})
		return tok
	}
	if _, err := iss.Verify(at(testNow.Unix() + 1)); err != nil {
		t.Errorf("exp через секунду: %v; want nil", err)
	}
	expectInvalid(t, iss, "exp == now", at(testNow.Unix()))
	expectInvalid(t, iss, "exp в прошлом", at(1))
}

func TestMalformed(t *testing.T) {
	iss := newIssuer()
	good, _ := iss.Sign(Claims{Sub: "a", Aud: "api", Exp: future()})
	parts := strings.Split(good, ".")
	payload := fmt.Sprintf(`{"sub":"a","aud":"api","exp":%d}`, future())
	cases := map[string]string{
		"четыре части":      good + ".x",
		"плохой base64":     parts[0] + ".!!!." + parts[2],
		"заголовок не JSON": forge(testKey, `not json`, payload),
		"данные не JSON":    forge(testKey, `{"alg":"HS256"}`, `[1,2]`),
		"alg HS512":         forge(testKey, `{"alg":"HS512"}`, payload),
		"alg RS256":         forge(testKey, `{"alg":"RS256"}`, payload),
		"подменены данные":  parts[0] + "." + b64.EncodeToString([]byte(strings.Replace(payload, `"a"`, `"admin"`, 1))) + "." + parts[2],
	}
	for name, tok := range cases {
		expectInvalid(t, iss, name, tok)
	}
}

func TestRealClock(t *testing.T) {
	iss := &Issuer{Key: testKey, Audience: "api"}
	tok, _ := iss.Sign(Claims{Sub: "a", Aud: "api", Exp: time.Now().Add(time.Hour).Unix()})
	if _, err := iss.Verify(tok); err != nil {
		t.Errorf("Now == nil, exp через час: %v; want nil", err)
	}
	old, _ := iss.Sign(Claims{Sub: "a", Aud: "api", Exp: time.Now().Add(-time.Hour).Unix()})
	expectInvalid(t, iss, "Now == nil, exp час назад", old)
}

// Неподписанный токен с alg "none" — классика: сервер,
// который берёт алгоритм из заголовка, пропускает его.
func TestExploitAlgNone(t *testing.T) {
	iss := newIssuer()
	payload := fmt.Sprintf(`{"sub":"admin","aud":"api","exp":%d}`, future())
	for _, alg := range []string{"none", "None", "NONE", "nOnE", ""} {
		tok := forge(nil, `{"alg":"`+alg+`","typ":"JWT"}`, payload)
		if c, err := iss.Verify(tok); err == nil {
			t.Errorf("alg %q без подписи принят: %+v", alg, c)
		}
	}
}

// Токен без exp живёт вечно: украденный однажды, он не
// протухает никогда.
func TestExploitNoExpiry(t *testing.T) {
	iss := newIssuer()
	tok, err := iss.Sign(Claims{Sub: "alice", Aud: "api"})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if c, err := iss.Verify(tok); err == nil {
		t.Errorf("токен без exp принят: %+v", c)
	}
	neg := forge(testKey, `{"alg":"HS256","typ":"JWT"}`, `{"sub":"alice","aud":"api","exp":-1}`)
	if c, err := iss.Verify(neg); err == nil {
		t.Errorf("токен с exp = -1 принят: %+v", c)
	}
}

// Токен, выпущенный для другого сервиса с тем же ключом,
// предъявляют нашему.
func TestExploitAudience(t *testing.T) {
	billing := &Issuer{Key: testKey, Audience: "billing", Now: func() time.Time { return testNow }}
	for _, aud := range []string{"billing", "", "API", "api "} {
		tok, err := billing.Sign(Claims{Sub: "alice", Aud: aud, Exp: future()})
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		if c, err := newIssuer().Verify(tok); err == nil {
			t.Errorf("токен с aud %q принят сервисом api: %+v", aud, c)
		}
	}
}

// Секрет из словаря подбирается офлайн по любому токену, после
// чего атакующий подписывает что угодно.
func TestExploitWeakKey(t *testing.T) {
	payload := fmt.Sprintf(`{"sub":"admin","aud":"api","exp":%d}`, future())
	dictionary := []string{"secret", "changeme", "jwt-secret", "0123456789abcdef0123456789abcde"}
	for _, k := range dictionary {
		iss := &Issuer{Key: []byte(k), Audience: "api", Now: func() time.Time { return testNow }}
		if tok, err := iss.Sign(Claims{Sub: "bob", Aud: "api", Exp: future()}); !errors.Is(err, ErrWeakKey) {
			t.Errorf("Sign с ключом %q (%d байт) = %q, %v; want ErrWeakKey", k, len(k), tok, err)
		}
		c, err := iss.Verify(forge([]byte(k), `{"alg":"HS256","typ":"JWT"}`, payload))
		if err == nil {
			t.Errorf("токен, подписанный подобранным ключом %q, принят: %+v", k, c)
		} else if !errors.Is(err, ErrWeakKey) {
			t.Errorf("Verify с ключом %q: %v; want ErrWeakKey", k, err)
		}
	}
}

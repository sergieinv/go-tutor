package task

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	testKey = []byte("0123456789abcdef0123456789abcdef") // 32 байта
	testNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
)

func newIssuer() *Issuer {
	return &Issuer{Key: testKey, Audience: "api", Now: func() time.Time { return testNow }}
}

func TestSignVerify(t *testing.T) {
	iss := newIssuer()
	c := Claims{Sub: "alice", Aud: "api", Exp: testNow.Add(time.Hour).Unix()}
	tok, err := iss.Sign(c)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if n := strings.Count(tok, "."); n != 2 {
		t.Fatalf("токен %q: %d точек; want 2", tok, n)
	}
	got, err := iss.Verify(tok)
	if err != nil || got != c {
		t.Fatalf("Verify(Sign(%+v)) = %+v, %v; want те же данные и nil", c, got, err)
	}
}

func TestRejects(t *testing.T) {
	iss := newIssuer()
	good, _ := iss.Sign(Claims{Sub: "alice", Aud: "api", Exp: testNow.Add(time.Hour).Unix()})
	expired, _ := iss.Sign(Claims{Sub: "alice", Aud: "api", Exp: testNow.Add(-time.Second).Unix()})
	other := &Issuer{Key: []byte("fedcba9876543210fedcba9876543210"), Audience: "api"}
	foreign, _ := other.Sign(Claims{Sub: "alice", Aud: "api", Exp: testNow.Add(time.Hour).Unix()})

	cases := map[string]string{
		"пустой":           "",
		"две части":        "a.b",
		"истёк":            expired,
		"чужой ключ":       foreign,
		"подпись обрезана": good[:len(good)-2],
	}
	for name, tok := range cases {
		c, err := iss.Verify(tok)
		if !errors.Is(err, ErrInvalid) || c != (Claims{}) {
			t.Errorf("%s: Verify = %+v, %v; want пустые Claims и ErrInvalid", name, c, err)
		}
	}
}

package task

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestSignVectors(t *testing.T) {
	tests := []struct {
		payload, key, want string
	}{
		{"", "k", ".i7mQxAp9YcuXWXqUISUCW-UKyL63RDbjc1uYiTp_ZiA"},
		{"\xfb\xff", "k", "-_8.S5_qpcVAE9wOwydu3r9NFXOaU0i1MoPKQrXcSdkhYBA"},
	}
	for _, tt := range tests {
		if got := Sign([]byte(tt.payload), []byte(tt.key)); got != tt.want {
			t.Errorf("Sign(%q, %q) = %q, want %q", tt.payload, tt.key, got, tt.want)
		}
	}
}

func TestVerifyEmptyPayload(t *testing.T) {
	got, err := Verify(".i7mQxAp9YcuXWXqUISUCW-UKyL63RDbjc1uYiTp_ZiA", []byte("k"))
	if err != nil || len(got) != 0 {
		t.Errorf("Verify(пустой payload) = %q, %v; want пустой payload, nil", got, err)
	}
}

func TestVerifyTampered(t *testing.T) {
	key := []byte("secret")
	tok := Sign([]byte(`{"user":"anna"}`), key)
	p, s, _ := strings.Cut(tok, ".")
	forged := Sign([]byte(`{"user":"root"}`), []byte("other"))
	fp, fs, _ := strings.Cut(forged, ".")
	bad := []string{
		"",
		".",
		p,
		p + ".",
		"." + s,
		p + "." + s + ".x",
		p + ".." + s,
		fp + "." + s,
		p + "." + fs,
		p + "=." + s,
		p + "." + s + "=",
		strings.ReplaceAll(strings.ReplaceAll(tok, "-", "+"), "_", "/"),
		p + "." + s[:len(s)-2],
		p + " ." + s,
		p + "." + s + "AA",
	}
	for _, in := range bad {
		if in == tok {
			continue // замена алфавита могла ничего не изменить
		}
		got, err := Verify(in, key)
		if !errors.Is(err, ErrInvalid) || got != nil {
			t.Errorf("Verify(%q) = %q, %v; want nil, ErrInvalid", in, got, err)
		}
	}
}

func TestVerifyBinary(t *testing.T) {
	key := []byte{0, 1, 2}
	payload := []byte{0xfb, 0xff, 0x00, 0x2e} // 0x2e — это '.'
	got, err := Verify(Sign(payload, key), key)
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("Verify(бинарный payload) = %v, %v; want %v", got, err, payload)
	}
	if strings.Count(Sign(payload, key), ".") != 1 {
		t.Errorf("в токене должна быть ровно одна точка: %q", Sign(payload, key))
	}
}

// flipPadBit меняет младший бит последнего символа: это «лишний» бит,
// который нестрогий декодер отбрасывает, и байты не меняются.
func flipPadBit(t *testing.T, part string) string {
	t.Helper()
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	i := strings.IndexByte(alphabet, part[len(part)-1])
	if i < 0 {
		t.Fatalf("не base64url: %q", part)
	}
	return part[:len(part)-1] + string(alphabet[i^1])
}

// Другая запись тех же байт не должна проходить с той же подписью.
func TestVerifyStrictDecoding(t *testing.T) {
	key := []byte("secret")
	tok := Sign([]byte("ab"), key) // 2 байта → 3 символа, 2 лишних бита
	p, s, _ := strings.Cut(tok, ".")
	if _, err := Verify(tok, key); err != nil {
		t.Fatalf("Verify(подлинный токен) = %v", err)
	}
	bad := []string{
		flipPadBit(t, p) + "." + s, // лишние биты в payload
		p + "." + flipPadBit(t, s), // лишние биты в подписи (32 байта → 43 символа)
		p[:1] + "\n" + p[1:] + "." + s,
		p + "." + s[:5] + "\r\n" + s[5:],
		p + "." + s + "\n",
		"\r" + p + "." + s,
	}
	for _, in := range bad {
		got, err := Verify(in, key)
		if !errors.Is(err, ErrInvalid) || got != nil {
			t.Errorf("Verify(%q) = %q, %v; want nil, ErrInvalid: токен отличается от выданного Sign", in, got, err)
		}
	}
}

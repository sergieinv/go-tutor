package task

import (
	"bytes"
	"errors"
	"testing"
)

func TestSign(t *testing.T) {
	got := Sign([]byte(`{"user":"anna"}`), []byte("secret"))
	want := "eyJ1c2VyIjoiYW5uYSJ9.Nmj9APAzS-D1t9LfigNYIMAeU2W4yQzMMsnWaC3SnGM"
	if got != want {
		t.Errorf("Sign = %q, want %q", got, want)
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	key := []byte("k1")
	payload := []byte("заказ 42")
	got, err := Verify(Sign(payload, key), key)
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("Verify(Sign(p)) = %q, %v; want %q, nil", got, err, payload)
	}
}

func TestVerifyWrongKey(t *testing.T) {
	tok := Sign([]byte("data"), []byte("k1"))
	got, err := Verify(tok, []byte("k2"))
	if !errors.Is(err, ErrInvalid) || got != nil {
		t.Errorf("Verify с чужим ключом = %q, %v; want nil, ErrInvalid", got, err)
	}
}

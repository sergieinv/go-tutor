package task

import (
	"bytes"
	"errors"
	"testing"
)

var key = bytes.Repeat([]byte{0x42}, 32)

func TestSealOpen(t *testing.T) {
	msg := []byte("card=4111111111111111")
	box, err := Seal(key, msg, []byte("user:42"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if len(box) != 12+len(msg)+16 {
		t.Errorf("len(box) = %d; want 12 + %d + 16", len(box), len(msg))
	}
	got, err := Open(key, box, []byte("user:42"))
	if err != nil || !bytes.Equal(got, msg) {
		t.Errorf("Open(Seal(%q)) = %q, %v", msg, got, err)
	}
}

func TestKeySize(t *testing.T) {
	if _, err := Seal(key[:16], []byte("x"), nil); !errors.Is(err, ErrKeySize) {
		t.Errorf("Seal с ключом 16 байт: %v; want ErrKeySize", err)
	}
}

func TestSignVerify(t *testing.T) {
	msg := []byte("/files/report.pdf?exp=1767229200")
	tag := Sign(key, msg)
	if len(tag) != 32 || !Verify(key, msg, tag) {
		t.Fatalf("Sign = %x; Verify(Sign) = %v; want 32 байта и true", tag, Verify(key, msg, tag))
	}
	if Verify(key, []byte("/files/salary.xls?exp=1767229200"), tag) {
		t.Error("тег одного сообщения подошёл к другому")
	}
}

package task

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
)

// mustRoundTrip — контроль перед атакой: шифрование работает.
func mustRoundTrip(t *testing.T, msg, aad []byte) []byte {
	t.Helper()
	box, err := Seal(key, msg, aad)
	if err != nil {
		t.Fatalf("Seal(%q): %v", msg, err)
	}
	got, err := Open(key, box, aad)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("Open(Seal(%q)) = %q, %v", msg, got, err)
	}
	return box
}

func TestFormatIsGCM(t *testing.T) {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	for _, msg := range [][]byte{nil, []byte("a"), bytes.Repeat([]byte("секрет"), 1000)} {
		box := mustRoundTrip(t, msg, []byte("aad"))
		if len(box) != 12+len(msg)+16 {
			t.Errorf("len(box) = %d; want 12 + %d + 16", len(box), len(msg))
			continue
		}
		got, err := gcm.Open(nil, box[:12], box[12:], []byte("aad"))
		if err != nil || !bytes.Equal(got, msg) {
			t.Errorf("эталонный AES-256-GCM не открыл box (nonce — первые 12 байт): %v", err)
		}
	}
}

func TestKeySizes(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33, 64} {
		k := bytes.Repeat([]byte{1}, n)
		if box, err := Seal(k, []byte("x"), nil); !errors.Is(err, ErrKeySize) || box != nil {
			t.Errorf("Seal, ключ %d байт = %x, %v; want nil, ErrKeySize", n, box, err)
		}
		if pt, err := Open(k, make([]byte, 40), nil); !errors.Is(err, ErrKeySize) || pt != nil {
			t.Errorf("Open, ключ %d байт = %x, %v; want nil, ErrKeySize", n, pt, err)
		}
	}
}

func TestOpenGarbage(t *testing.T) {
	mustRoundTrip(t, []byte("x"), nil)
	for n := range 40 {
		if pt, err := Open(key, make([]byte, n), nil); err == nil || pt != nil {
			t.Errorf("Open(%d нулевых байт) = %x, %v; want nil и ошибку", n, pt, err)
		}
	}
	box := mustRoundTrip(t, []byte("hello"), nil)
	other := bytes.Repeat([]byte{7}, 32)
	if pt, err := Open(other, box, nil); err == nil || pt != nil {
		t.Errorf("Open чужим ключом = %q, %v; want nil и ошибку", pt, err)
	}
}

func TestSignIsHMAC(t *testing.T) {
	for _, k := range [][]byte{key, []byte("short"), nil} {
		msg := []byte("msg")
		m := hmac.New(sha256.New, k)
		m.Write(msg)
		want := m.Sum(nil)
		if got := Sign(k, msg); !bytes.Equal(got, want) {
			t.Errorf("Sign(%q, %q) = %x; want HMAC-SHA256 %x", k, msg, got, want)
		}
		if !Verify(k, msg, want) {
			t.Errorf("Verify(%q) отверг верный тег", k)
		}
	}
	if Verify(bytes.Repeat([]byte{1}, 32), []byte("msg"), Sign(key, []byte("msg"))) {
		t.Error("тег, посчитанный другим ключом, принят")
	}
}

func TestConcurrentSeal(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				box, err := Seal(key, []byte("x"), nil)
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := Open(key, box, nil); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}

// Повтор nonce: зная один открытый текст, атакующий
// восстанавливает другой по XOR шифртекстов, без ключа.
func TestExploitNonceReuse(t *testing.T) {
	known := []byte("pay alice    100")
	secret := []byte("pay mallory 9000")
	b1 := mustRoundTrip(t, known, nil)
	b2 := mustRoundTrip(t, secret, nil)
	if len(b1) == len(b2) && len(b1) >= 12+len(known) {
		got := make([]byte, len(known))
		for i := range got {
			got[i] = b1[12+i] ^ b2[12+i] ^ known[i]
		}
		if bytes.Equal(got, secret) {
			t.Fatalf("второй текст восстановлен без ключа: %q — nonce повторился", got)
		}
	}
	nonces := map[string]bool{}
	boxes := map[string]bool{}
	for range 500 {
		box := mustRoundTrip(t, []byte("одно и то же"), nil)
		if len(box) < 12 {
			t.Fatalf("len(box) = %d", len(box))
		}
		if nonces[string(box[:12])] || boxes[string(box)] {
			t.Fatalf("за 500 вызовов Seal повторился nonce или шифртекст")
		}
		nonces[string(box[:12])] = true
		boxes[string(box)] = true
	}
}

// Изменение любого бита — в nonce, шифртексте или теге —
// должно обнаруживаться, а не давать изменённый текст.
func TestExploitBitFlip(t *testing.T) {
	msg := []byte("amount=100;to=alice")
	box := mustRoundTrip(t, msg, []byte("aad"))
	for i := range box {
		for _, bit := range []byte{0x01, 0x80} {
			bad := bytes.Clone(box)
			bad[i] ^= bit
			if pt, err := Open(key, bad, []byte("aad")); err == nil {
				t.Fatalf("изменён байт %d (бит %#x), Open принял: %q", i, bit, pt)
			}
		}
	}
	for _, n := range []int{len(box) - 1, len(box) - 16, 12 + len(msg)} {
		if pt, err := Open(key, box[:n], []byte("aad")); err == nil {
			t.Fatalf("обрезанный до %d байт box принят: %q", n, pt)
		}
	}
	if pt, err := Open(key, append(bytes.Clone(box), 0), []byte("aad")); err == nil {
		t.Fatalf("box с лишним байтом принят: %q", pt)
	}
}

// Шифртекст одного пользователя подставляют в запись другого.
func TestExploitAADSwap(t *testing.T) {
	box := mustRoundTrip(t, []byte("card=4111111111111111"), []byte("user:42"))
	for _, aad := range [][]byte{[]byte("user:7"), []byte("user:420"), nil} {
		if pt, err := Open(key, box, aad); err == nil {
			t.Errorf("box для user:42 открыт с aad %q: %q", aad, pt)
		}
	}
}

// Сравнение «по длине присланного тега» принимает пустой
// тег, а однобайтовый подбирается за 256 попыток.
func TestExploitShortTag(t *testing.T) {
	msg := []byte("/admin/delete?id=1")
	tag := Sign(key, msg)
	if !Verify(key, msg, tag) {
		t.Fatalf("Verify(Sign) = false — контроль не прошёл")
	}
	for n := range len(tag) {
		if Verify(key, msg, tag[:n]) {
			t.Fatalf("принят тег из первых %d байт", n)
		}
	}
	for b := range 256 {
		if Verify(key, msg, []byte{byte(b)}) {
			t.Fatalf("однобайтовый тег %#x подобран перебором", b)
		}
	}
	if Verify(key, msg, append(bytes.Clone(tag), 0)) {
		t.Fatal("принят тег с лишним байтом")
	}
}

// Пароль, токен сессии и подписанный токен решают разные
// задачи и строятся из разных примитивов: медленный хеш
// с солью, криптостойкая случайность и HMAC. Ни один из них
// не пишут сами — только собирают из готового.

package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Параметры argon2id — минимум из памятки OWASP: 19 МиБ,
// 2 прохода, 1 поток. Они записаны в саму строку хеша
// (формат PHC), поэтому их можно поднять позже, не ломая
// уже сохранённые хеши.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // в КиБ
	argonThreads = 1
	argonKeyLen  = 32
)

var b64 = base64.RawStdEncoding

func hashArgon2id(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt) // crypto/rand; с Go 1.24 ошибку не возвращает
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// Проверка берёт параметры и соль из строки, пересчитывает
// ключ и сравнивает за постоянное время. Пустой или короткий
// ключ в строке — отказ: `ConstantTimeCompare` двух пустых
// срезов вернул бы 1, то есть «пароль верный».
func verifyArgon2id(phc, password string) bool {
	parts := strings.Split(phc, "$") // "", argon2id, v=19, m=…,t=…,p=…, соль, ключ
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) < 16 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// В хранилище сессий — SHA-256 токена, а не сам токен:
// утечка базы не даёт готовых сессий. Медленный хеш здесь
// не нужен — у токена из `crypto/rand` и так 130 бит энтропии.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Подписанный токен: данные + HMAC-SHA256 от них. Подделать
// подпись без ключа нельзя, сравнение — `hmac.Equal`.
func sign(key []byte, user string, exp int64) string {
	payload := user + "|" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verify(key []byte, token string, now int64) (string, bool) {
	payload, sig, ok := strings.Cut(token, ".")
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if !ok || err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return "", false
	}
	// Срок проверяем только после подписи: до неё данным не верим.
	user, expStr, _ := strings.Cut(payload, "|")
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now >= exp {
		return "", false
	}
	return user, true
}

func main() {
	// bcrypt: соль и стоимость внутри строки хеша.
	// Больше 72 байт пароля bcrypt не принимает.
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.DefaultCost)
	fmt.Println("bcrypt:", string(hash[:7]), "длина", len(hash))
	fmt.Println("  верный:", bcrypt.CompareHashAndPassword(hash, []byte("correct horse")) == nil)
	fmt.Println("  неверный:", bcrypt.CompareHashAndPassword(hash, []byte("wrong")))
	_, err := bcrypt.GenerateFromPassword([]byte(strings.Repeat("я", 37)), bcrypt.DefaultCost)
	fmt.Println("  74 байта:", err)

	phc := hashArgon2id("correct horse")
	fmt.Println("argon2id:", strings.Join(strings.Split(phc, "$")[:4], "$")+"$…")
	fmt.Println("  верный:", verifyArgon2id(phc, "correct horse"),
		"неверный:", verifyArgon2id(phc, "wrong"))

	// Токен сессии — только из `crypto/rand`.
	token := rand.Text()
	sessions := map[string]string{hashToken(token): "alice"}
	fmt.Printf("токен: %d символов, в хранилище: %d\n", len(token), len(hashToken(token)))
	fmt.Println("  сессия:", sessions[hashToken(token)])

	key := make([]byte, 32)
	rand.Read(key)
	const now = 1_767_225_600 // 2026-01-01 00:00 UTC
	signed := sign(key, "alice", now+3600)
	fmt.Println("подписан:", signed[:strings.IndexByte(signed, '.')])
	check := func(what, tok string, at int64) {
		user, ok := verify(key, tok, at)
		fmt.Printf("  %s: %q %v\n", what, user, ok)
	}
	check("в срок", signed, now)
	check("подмена", strings.Replace(signed, "alice", "admin", 1), now)
	check("истёк", signed, now+3600)
}

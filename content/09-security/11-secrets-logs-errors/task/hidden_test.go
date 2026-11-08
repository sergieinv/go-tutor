package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const raw = "hunter2-Xq9"

func TestExploitSecretViaFmt(t *testing.T) {
	type Config struct {
		Addr  string
		Token Secret
	}
	s := Secret(raw)
	cfg := Config{Addr: "db:5432", Token: s}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%10.3s"} {
		for _, arg := range []any{s, cfg, &cfg} {
			got := fmt.Sprintf(format, arg)
			if strings.Contains(got, raw) || strings.Contains(got, fmt.Sprintf("%x", raw)) {
				t.Errorf("fmt.Sprintf(%q, %T) раскрыл секрет: %s", format, arg, got)
			}
		}
	}
	if got := fmt.Sprintf("%v %#v", cfg, cfg); !strings.Contains(got, "db:5432") || !strings.Contains(got, "[REDACTED]") {
		t.Errorf("остальные поля печатаются как обычно, секрет — [REDACTED]: %s", got)
	}
}

func TestExploitSecretViaJSON(t *testing.T) {
	b, err := json.Marshal(struct {
		User  string
		Token Secret
	}{"bob", Secret(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"User":"bob","Token":"[REDACTED]"}` {
		t.Fatalf("json.Marshal = %s, want секрет как \"[REDACTED]\"", b)
	}
}

func TestExploitSecretInLogs(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf)
	log.Info("a", "password", raw)
	log.Info("b", "PASSWORD", raw, "Api_Key", raw)
	log.Info("c", slog.Group("req", "Authorization", "Bearer "+raw, slog.Group("h", "Cookie", raw)))
	log.Info("d", "x", Secret(raw))
	log.Info("e", "cfg", struct{ DSN Secret }{Secret(raw)})
	log.With("token", raw).Info("f")
	log.WithGroup("g").Info("g", "secret", raw)
	if strings.Contains(buf.String(), raw) {
		t.Fatalf("секрет в логе:\n%s", buf.String())
	}
	if n := strings.Count(buf.String(), "[REDACTED]"); n != 9 {
		t.Errorf("ожидалось 9 значений [REDACTED], найдено %d:\n%s", n, buf.String())
	}
}

func TestExploitErrorDetails(t *testing.T) {
	var buf bytes.Buffer
	cause := errors.New("dial tcp 10.0.0.5:5432: password authentication failed for user \"app\"")
	errs := []error{
		fmt.Errorf("load user: %w", cause),
		&PublicError{Status: 404, Msg: "user not found", Err: cause},
		fmt.Errorf("handler: %w", &PublicError{Status: 409, Msg: "conflict", Err: cause}),
	}
	for _, e := range errs {
		h := Handle(NewLogger(&buf), func(w http.ResponseWriter, r *http.Request) error { return e })
		rec := serve(h, "GET", "/users/1")
		if rec.Code < 400 {
			t.Errorf("%v: код %d, want ошибку", e, rec.Code)
		}
		if b := rec.Body.String(); strings.Contains(b, "10.0.0.5") || strings.Contains(b, "password") {
			t.Errorf("ответ клиенту раскрывает внутреннюю ошибку: %s", b)
		}
	}
	if !strings.Contains(buf.String(), "10.0.0.5") {
		t.Errorf("подробности должны попасть в лог:\n%s", buf.String())
	}
}

func TestExploitPanicDetails(t *testing.T) {
	var buf bytes.Buffer
	h := Handle(NewLogger(&buf), func(w http.ResponseWriter, r *http.Request) error {
		panic("open /etc/app/master.key: permission denied")
	})
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if v := recover(); v != nil {
				t.Fatalf("паника вышла из Handle: %v", v)
			}
		}()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	}()
	if rec.Code != 500 {
		t.Fatalf("код %d, want 500", rec.Code)
	}
	if b := rec.Body.String(); strings.Contains(b, "master.key") || strings.Contains(b, "goroutine") {
		t.Fatalf("ответ раскрывает панику: %s", b)
	}
}

func TestPanicLogged(t *testing.T) {
	var buf bytes.Buffer
	h := Handle(NewLogger(&buf), func(w http.ResponseWriter, r *http.Request) error {
		panic(fmt.Sprintf("boom %d", 42))
	})
	rec := serve(h, "GET", "/")
	id := rec.Header().Get("X-Request-ID")
	body := errorBody(t, rec)
	if body["error"] != "internal error" || body["request_id"] != id || id == "" {
		t.Fatalf("тело %v, want error=internal error и request_id из заголовка", body)
	}
	logs := records(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("записей в логе %d, want 1:\n%s", len(logs), buf.String())
	}
	l := logs[0]
	stack, _ := l["stack"].(string)
	if l["level"] != "ERROR" || l["msg"] != "panic" || l["request_id"] != id || l["panic"] != "boom 42" || !strings.Contains(stack, "goroutine") {
		t.Fatalf("запись о панике %v: want level=ERROR msg=panic request_id panic=boom 42 и stack", l)
	}
}

func TestAbortHandlerRepanics(t *testing.T) {
	h := Handle(NewLogger(&bytes.Buffer{}), func(w http.ResponseWriter, r *http.Request) error {
		panic(http.ErrAbortHandler)
	})
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recover() = %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestPublicErrorCauseInLog(t *testing.T) {
	var buf bytes.Buffer
	h := Handle(NewLogger(&buf), func(w http.ResponseWriter, r *http.Request) error {
		return &PublicError{Status: 400, Msg: "bad date", Err: errors.New(`parsing time "x"`)}
	})
	rec := serve(h, "GET", "/")
	if body := errorBody(t, rec); rec.Code != 400 || body["error"] != "bad date" {
		t.Fatalf("ответ %d %v, want 400 bad date", rec.Code, body)
	}
	logs := records(t, &buf)
	if len(logs) != 1 || logs[0]["err"] != `bad date: parsing time "x"` {
		t.Fatalf("в логе нужна полная ошибка err.Error():\n%s", buf.String())
	}
}

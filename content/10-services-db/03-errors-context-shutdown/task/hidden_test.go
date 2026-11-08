package task

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// dbError — «настоящая» ошибка базы с подробностями, которые
// нельзя показывать клиенту.
type dbError struct{ detail string }

func (e *dbError) Error() string { return "db: " + e.detail }

// errorLogs возвращает значения атрибута err у записей уровня ERROR.
func errorLogs(t *testing.T, e *env) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(strings.NewReader(e.logs.String()))
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("строка лога не JSON: %q", sc.Text())
		}
		if rec["level"] == "ERROR" {
			s, _ := rec["err"].(string)
			out = append(out, s)
		}
	}
	return out
}

func TestValidationBeforeStore(t *testing.T) {
	st := newStore()
	svc := NewService(st)
	ctx := t.Context()
	check := func(name string, err error, field string) {
		t.Helper()
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != field || ve.Msg != "must be positive" {
			t.Errorf("%s: error = %v, want *ValidationError{Field: %q, Msg: \"must be positive\"}", name, err, field)
		}
	}
	it, err := svc.Item(ctx, 0)
	check("Item(0)", err, "id")
	if it != (Item{}) {
		t.Errorf("Item(0) вернул %+v, want Item{}", it)
	}
	_, err = svc.Item(ctx, -1)
	check("Item(-1)", err, "id")
	check("Reserve(0, 1)", svc.Reserve(ctx, 0, 1), "id")
	check("Reserve(0, 0)", svc.Reserve(ctx, 0, 0), "id")
	check("Reserve(1, 0)", svc.Reserve(ctx, 1, 0), "qty")
	check("Reserve(1, -2)", svc.Reserve(ctx, 1, -2), "qty")
	if n := st.callCount(); n != 0 {
		t.Errorf("при неверном вводе хранилище вызвано %d раз, want 0", n)
	}
}

func TestServiceKeepsCause(t *testing.T) {
	st := newStore()
	st.fail = &dbError{"connection reset"}
	svc := NewService(st)
	it, err := svc.Item(t.Context(), 5)
	var de *dbError
	if !errors.As(err, &de) || de.detail != "connection reset" || it != (Item{}) {
		t.Errorf("Item(5) = (%+v, %v), want (Item{}, ошибка, из которой errors.As достаёт *dbError)", it, err)
	}
	if err != nil && !strings.Contains(err.Error(), "5") {
		t.Errorf("Item(5) error %q не называет id", err)
	}
	err = svc.Reserve(t.Context(), 6, 1)
	if !errors.As(err, &de) || !strings.Contains(err.Error(), "6") {
		t.Errorf("Reserve(6, 1) error = %v, want обёрнутую *dbError с id в тексте", err)
	}
}

func TestInternalError(t *testing.T) {
	for _, req := range [][3]string{{"GET", "/items/1", ""}, {"POST", "/items/1/reserve", `{"qty": 1}`}} {
		e := newEnv(t)
		e.store.fail = &dbError{"disk I/O error at /var/lib/stock.db"}
		rec := e.do(t.Context(), req[0], req[1], req[2])
		if rec.Code != 500 || rec.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s %s при сбое базы: %d %q, want 500 application/json", req[0], req[1], rec.Code, rec.Header().Get("Content-Type"))
		}
		if got := errorBody(t, rec); got != "internal error" || strings.Contains(rec.Body.String(), "disk") {
			t.Errorf("%s %s: тело %s — подробности сбоя ушли клиенту", req[0], req[1], rec.Body.String())
		}
		logs := errorLogs(t, e)
		if len(logs) != 1 || !strings.Contains(logs[0], "disk I/O error at /var/lib/stock.db") {
			t.Errorf("%s %s: записи ERROR с err = %q, want одну с текстом ошибки базы", req[0], req[1], logs)
		}
	}
}

func TestExpectedErrorsNotLogged(t *testing.T) {
	e := newEnv(t)
	e.do(t.Context(), "GET", "/items/9", "")
	e.do(t.Context(), "GET", "/items/x", "")
	e.do(t.Context(), "POST", "/items/1/reserve", `{"qty": 50}`)
	e.do(t.Context(), "POST", "/items/1/reserve", `{"qty": -1}`)
	if logs := errorLogs(t, e); len(logs) != 0 {
		t.Errorf("ожидаемые ошибки (404, 400, 409) записаны как ERROR: %q", logs)
	}
}

// serve выполняет запрос в горутине и ждёт ответа не дольше 5 с:
// если контекст не дошёл до хранилища, тест не зависнет.
func serve(t *testing.T, e *env, ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- e.do(ctx, method, path, body) }()
	select {
	case rec := <-done:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatalf("%s %s не завершился через 5 с после отмены: контекст запроса не дошёл до хранилища", method, path)
		return nil
	}
}

func TestDeadlineReachesStore(t *testing.T) {
	for _, req := range [][3]string{{"GET", "/items/1", ""}, {"POST", "/items/1/reserve", `{"qty": 1}`}} {
		e := newEnv(t)
		e.store.block = true
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		rec := serve(t, e, ctx, req[0], req[1], req[2])
		cancel()
		if rec.Code != 503 || errorBody(t, rec) != "timeout" {
			t.Errorf("%s %s по дедлайну: %d %s, want 503 timeout", req[0], req[1], rec.Code, rec.Body.String())
		}
		if logs := errorLogs(t, e); len(logs) != 0 {
			t.Errorf("%s %s: таймаут записан как ERROR: %q", req[0], req[1], logs)
		}
	}
}

func TestClientCancel(t *testing.T) {
	e := newEnv(t)
	e.store.block = true
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		for e.store.callCount() == 0 && ctx.Err() == nil {
			time.Sleep(time.Millisecond)
		}
		cancel() // клиент ушёл, пока хранилище работало
	}()
	rec := serve(t, e, ctx, "GET", "/items/1", "")
	if rec.Code != 503 || errorBody(t, rec) != "timeout" {
		t.Errorf("GET /items/1 при отмене: %d %s, want 503 timeout", rec.Code, rec.Body.String())
	}
}

type ctxKey struct{}

func TestContextValueReachesStore(t *testing.T) {
	for _, req := range [][3]string{{"GET", "/items/1", ""}, {"POST", "/items/1/reserve", `{"qty": 1}`}} {
		e := newEnv(t)
		ctx := context.WithValue(t.Context(), ctxKey{}, "req-7")
		e.do(ctx, req[0], req[1], req[2])
		e.store.mu.Lock()
		got := e.store.lastCtx
		e.store.mu.Unlock()
		if got == nil || got.Value(ctxKey{}) != "req-7" {
			t.Errorf("%s %s: хранилище получило не контекст запроса", req[0], req[1])
		}
	}
}

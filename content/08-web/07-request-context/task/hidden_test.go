package task

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

type testKey struct{}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// serveCtx вызывает хендлер с контекстом запроса ctx и ждёт его не дольше 2 с.
func serveCtx(t *testing.T, fetch Fetcher, timeout time.Duration, ctx context.Context, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", target, nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		Handler(fetch, timeout).ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
		return rec
	case <-time.After(2 * time.Second):
		t.Fatal("хендлер не вернулся за 2 с: он не должен ждать fetch после отмены или истечения лимита")
		return nil
	}
}

func TestFetchGetsRequestContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), testKey{}, "req-42")
	var gotID, gotValue string
	var hasDeadline bool
	var left time.Duration
	fetch := func(ctx context.Context, id string) (string, error) {
		gotID = id
		gotValue, _ = ctx.Value(testKey{}).(string)
		var dl time.Time
		dl, hasDeadline = ctx.Deadline()
		left = time.Until(dl)
		return "ok", nil
	}
	rec := serveCtx(t, fetch, 500*time.Millisecond, ctx, "/?id=a%20b")
	check(t, rec, 200, "ok")
	if gotID != "a b" {
		t.Errorf("fetch получил id %q, want %q", gotID, "a b")
	}
	if gotValue != "req-42" {
		t.Errorf("в контексте fetch нет значения запроса: контекст должен быть производным от r.Context()")
	}
	if !hasDeadline || left <= 0 || left > 500*time.Millisecond {
		t.Errorf("у контекста fetch нет дедлайна ≈ timeout (deadline=%v, осталось %v)", hasDeadline, left)
	}

	serveCtx(t, fetch, 0, ctx, "/?id=x")
	if hasDeadline {
		t.Errorf("timeout = 0: у контекста fetch не должно быть дедлайна")
	}
	serveCtx(t, fetch, -time.Second, ctx, "/?id=x")
	if hasDeadline {
		t.Errorf("timeout < 0: у контекста fetch не должно быть дедлайна")
	}
}

func TestMissingIDDoesNotFetch(t *testing.T) {
	called := false
	fetch := func(context.Context, string) (string, error) { called = true; return "", nil }
	check(t, serveCtx(t, fetch, time.Second, context.Background(), "/?id="), 400, "missing id\n")
	if called {
		t.Error("fetch вызван без id")
	}
}

func TestClientCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	rec := serveCtx(t, wait(time.Hour, "never"), time.Second, ctx, "/?id=1")
	check(t, rec, 503, "request canceled\n")
}

func TestRequestDeadlineIsNotOurTimeout(t *testing.T) {
	// У самого запроса дедлайн короче нашего лимита (например, его
	// поставил middleware): запрос завершён — 503, а не 504.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	rec := serveCtx(t, wait(time.Hour, "never"), time.Second, ctx, "/?id=1")
	check(t, rec, 503, "request canceled\n")
}

func TestStubbornFetcher(t *testing.T) {
	before := runtime.NumGoroutine()
	release := make(chan struct{})
	// fetch игнорирует контекст и отвечает, только когда тест разрешит.
	stubborn := func(context.Context, string) (string, error) {
		<-release
		return "late", nil
	}
	start := time.Now()
	rec := serveCtx(t, stubborn, 30*time.Millisecond, context.Background(), "/?id=1")
	check(t, rec, 504, "timeout\n")
	if time.Since(start) < 30*time.Millisecond {
		t.Errorf("504 раньше лимита")
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	rec = serveCtx(t, stubborn, 0, ctx, "/?id=1")
	check(t, rec, 503, "request canceled\n")

	close(release)
	if !waitFor(time.Second, func() bool { return runtime.NumGoroutine() <= before }) {
		t.Errorf("горутин до: %d, после: %d — опоздавший результат fetch некому принять", before, runtime.NumGoroutine())
	}
}

func TestErrorAfterTimeout(t *testing.T) {
	// fetch отвечает своей ошибкой, когда истёк наш лимит:
	// решает состояние контекста, а не текст ошибки.
	fetch := func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", errors.New("db: connection reset")
	}
	check(t, serveCtx(t, fetch, 20*time.Millisecond, context.Background(), "/?id=1"), 504, "timeout\n")

	notFoundLate := func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ErrNotFound
	}
	check(t, serveCtx(t, notFoundLate, 20*time.Millisecond, context.Background(), "/?id=1"), 504, "timeout\n")
}

func TestWrappedNotFound(t *testing.T) {
	fetch := func(_ context.Context, id string) (string, error) {
		return "", fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	check(t, serveCtx(t, fetch, time.Second, context.Background(), "/?id=1"), 404, "not found\n")
	check(t, serveCtx(t, fetch, 0, context.Background(), "/?id=1"), 404, "not found\n")
}

func TestManyRequests(t *testing.T) {
	h := Handler(wait(time.Millisecond, "v"), time.Second)
	for i := range 20 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", fmt.Sprintf("/?id=%d", i), nil))
		check(t, rec, 200, fmt.Sprintf("v:%d", i))
	}
}

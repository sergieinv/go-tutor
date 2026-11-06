package task

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

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

// searchAsync запускает Search и ждёт результата не дольше 2 с.
func searchAsync(t *testing.T, ctx context.Context, timeout time.Duration, backends ...Backend) (string, error) {
	t.Helper()
	type res struct {
		v   string
		err error
	}
	done := make(chan res, 1)
	go func() {
		v, err := Search(ctx, timeout, backends...)
		done <- res{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-time.After(2 * time.Second):
		t.Fatal("Search не вернулся за 2 с: он не должен ждать медленные бэкенды")
		return "", nil
	}
}

func TestSearchNoBackends(t *testing.T) {
	if _, err := Search(context.Background(), time.Second); !errors.Is(err, ErrNoBackends) {
		t.Fatalf("Search() без бэкендов = %v, want ErrNoBackends", err)
	}
}

func TestSearchErrorThenSuccess(t *testing.T) {
	got, err := Search(context.Background(), time.Second, failing(errors.New("boom")), after(20*time.Millisecond, "ok"))
	if err != nil || got != "ok" {
		t.Fatalf("Search = (%q, %v), want (\"ok\", nil): ошибка одного бэкенда не должна прерывать поиск", got, err)
	}
}

func TestSearchCancelsOthers(t *testing.T) {
	ctxs := make(chan context.Context, 3)
	spy := func(b Backend) Backend {
		return func(ctx context.Context) (string, error) {
			ctxs <- ctx
			return b(ctx)
		}
	}
	got, err := Search(context.Background(), time.Second,
		spy(after(5*time.Millisecond, "fast")), spy(after(time.Hour, "a")), spy(after(time.Hour, "b")))
	if err != nil || got != "fast" {
		t.Fatalf("Search = (%q, %v), want (\"fast\", nil)", got, err)
	}
	for range 3 {
		select {
		case ctx := <-ctxs:
			if ctx.Err() == nil {
				t.Fatal("после возврата Search контекст бэкенда не отменён")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("не все бэкенды запущены")
		}
	}
}

func TestSearchParentCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := searchAsync(t, ctx, time.Hour, after(time.Hour, "never"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Search после отмены ctx = %v, want context.Canceled", err)
	}
}

func TestSearchZeroTimeoutMeansNoLimit(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		got, err := Search(context.Background(), timeout, after(30*time.Millisecond, "ok"))
		if err != nil || got != "ok" {
			t.Errorf("Search(timeout=%v) = (%q, %v), want (\"ok\", nil): при timeout <= 0 лимита нет", timeout, got, err)
		}
	}
}

func TestSearchStubbornBackend(t *testing.T) {
	before := runtime.NumGoroutine()
	release := make(chan struct{})
	// Бэкенд игнорирует отмену и отвечает, только когда тест разрешит.
	stubborn := func(context.Context) (string, error) {
		<-release
		return "late", nil
	}

	got, err := searchAsync(t, context.Background(), time.Second, stubborn, after(10*time.Millisecond, "fast"))
	if err != nil || got != "fast" {
		t.Fatalf("Search = (%q, %v), want (\"fast\", nil)", got, err)
	}
	_, err = searchAsync(t, context.Background(), 30*time.Millisecond, stubborn)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Search с упрямым бэкендом = %v, want context.DeadlineExceeded", err)
	}

	close(release) // упрямые бэкенды отвечают — их горутины должны завершиться
	if !waitFor(time.Second, func() bool { return runtime.NumGoroutine() <= before }) {
		t.Errorf("горутин до: %d, после: %d — опоздавший ответ некому принять, горутина зависла", before, runtime.NumGoroutine())
	}
}

// Отмена ctx и ошибки бэкендов приходят одновременно:
// приоритет у ошибки контекста, а не у errors.Join.
func TestSearchCanceledBeatsBackendErrors(t *testing.T) {
	down := errors.New("backend down")
	failOnCancel := func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", down // ошибка бэкенда не оборачивает ctx.Err()
	}
	failNow := func(context.Context) (string, error) { return "", down }
	for range 50 {
		ctx, cancel := context.WithCancel(context.Background())
		go cancel()
		_, err := Search(ctx, 0, failOnCancel, failOnCancel, failOnCancel)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Search при отмене ctx = %v, want context.Canceled", err)
		}

		// ctx отменён заранее, а бэкенды сразу отвечают ошибкой:
		// оба события готовы одновременно.
		ctx, cancel = context.WithCancel(context.Background())
		cancel()
		_, err = Search(ctx, 0, failNow, failNow, failNow)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Search(отменённый ctx, бэкенды с ошибкой) = %v, want context.Canceled", err)
		}
	}
}

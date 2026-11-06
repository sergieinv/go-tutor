package task

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// safetyNet перехватывает тестовые сигналы, чтобы процесс тестов
// не завершился, даже если Main на них не подписалась.
func safetyNet(t *testing.T) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(ch) })
}

// waitCancel посылает сигнал самому процессу и ждёт отмены ctx.
func waitCancel(ctx context.Context, sig syscall.Signal) error {
	syscall.Kill(os.Getpid(), sig)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Second):
		return errors.New("сигнал не отменил контекст")
	}
}

func TestMainOK(t *testing.T) {
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error { return nil }, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Errorf("Main = %d, stderr %q; want 0 и пустой stderr", code, stderr.String())
	}
}

func TestMainError(t *testing.T) {
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error { return errors.New("boom") }, &stderr)
	if code != 1 || stderr.String() != "error: boom\n" {
		t.Errorf("Main = %d, stderr %q; want 1 и %q", code, stderr.String(), "error: boom\n")
	}
}

func TestMainSignal(t *testing.T) {
	safetyNet(t)
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error {
		return waitCancel(ctx, syscall.SIGUSR1)
	}, &stderr, syscall.SIGUSR1)
	if code != 0 || stderr.Len() != 0 {
		t.Errorf("Main = %d, stderr %q; want 0 — остановка по сигналу штатная", code, stderr.String())
	}
}

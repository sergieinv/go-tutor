package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
)

func TestMainDefaultSignals(t *testing.T) {
	safetyNet(t)
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error {
		return waitCancel(ctx, syscall.SIGTERM)
	}, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Errorf("Main = %d, stderr %q; want 0 — без sigs ловится SIGTERM", code, stderr.String())
	}
}

func TestMainSignalWrappedCanceled(t *testing.T) {
	safetyNet(t)
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error {
		if err := waitCancel(ctx, syscall.SIGUSR1); err != nil {
			return fmt.Errorf("worker stopped: %w", err)
		}
		return nil
	}, &stderr, syscall.SIGUSR1)
	if code != 0 || stderr.Len() != 0 {
		t.Errorf("Main = %d, stderr %q; want 0 — обёрнутый context.Canceled после сигнала", code, stderr.String())
	}
}

func TestMainSignalThenOtherError(t *testing.T) {
	safetyNet(t)
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error {
		if err := waitCancel(ctx, syscall.SIGUSR1); !errors.Is(err, context.Canceled) {
			return err
		}
		return errors.New("flush failed")
	}, &stderr, syscall.SIGUSR1)
	if code != 1 || stderr.String() != "error: flush failed\n" {
		t.Errorf("Main = %d, stderr %q; want 1 и %q", code, stderr.String(), "error: flush failed\n")
	}
}

func TestMainCanceledWithoutSignal(t *testing.T) {
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error { return context.Canceled }, &stderr)
	if code != 1 || stderr.String() != "error: context canceled\n" {
		t.Errorf("Main = %d, stderr %q; want 1 — сигнала не было", code, stderr.String())
	}
}

func TestMainPanic(t *testing.T) {
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error { panic("oops") }, &stderr)
	if code != 2 || stderr.String() != "panic: oops\n" {
		t.Errorf("Main = %d, stderr %q; want 2 и %q", code, stderr.String(), "panic: oops\n")
	}
	stderr.Reset()
	code = Main(func(ctx context.Context) error { panic(errors.New("bad state")) }, &stderr)
	if code != 2 || stderr.String() != "panic: bad state\n" {
		t.Errorf("Main = %d, stderr %q; want 2 и %q", code, stderr.String(), "panic: bad state\n")
	}
}

func TestMainContextAlive(t *testing.T) {
	var stderr strings.Builder
	code := Main(func(ctx context.Context) error {
		if ctx == nil || ctx.Err() != nil {
			return errors.New("контекст должен быть живым до сигнала")
		}
		return nil
	}, &stderr, syscall.SIGUSR1)
	if code != 0 {
		t.Errorf("Main = %d, stderr %q; want 0", code, stderr.String())
	}
}

package task

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunNoShell(t *testing.T) {
	got, err := Run(context.Background(), "", "echo", "$HOME", "a b", "*", "; ls")
	want := "$HOME a b * ; ls\n"
	if err != nil || got.Stdout != want {
		t.Errorf("Run(echo ...) = %+v, %v; want Stdout %q — аргументы не должны проходить через оболочку", got, err, want)
	}
}

func TestRunNotFound(t *testing.T) {
	_, err := Run(context.Background(), "", "no-such-program-gotutor")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("Run(нет программы) вернул %v, want exec.ErrNotFound", err)
	}
}

func TestRunTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, "", "/bin/sh", "-c", "while :; do :; done")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run с истёкшим таймаутом вернул %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Run ждал %v — процесс не был убит по таймауту", d)
	}
}

func TestRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, "", "/bin/sh", "-c", "echo should-not-run")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run с отменённым контекстом вернул %v, want context.Canceled", err)
	}
}

func TestRunLargeOutput(t *testing.T) {
	// Больше буфера пайпа (64 КБ) в оба потока одновременно.
	script := `i=0; while [ $i -lt 20000 ]; do echo line; echo errline >&2; i=$((i+1)); done; exit 1`
	got, err := Run(context.Background(), "", "/bin/sh", "-c", script)
	if err != nil {
		t.Fatalf("Run вернул %v", err)
	}
	if n := strings.Count(got.Stdout, "line\n"); n != 20000 {
		t.Errorf("строк в Stdout %d, want 20000", n)
	}
	if n := strings.Count(got.Stderr, "errline\n"); n != 20000 {
		t.Errorf("строк в Stderr %d, want 20000", n)
	}
	if got.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", got.ExitCode)
	}
}

func TestRunEmptyStdin(t *testing.T) {
	// Пустой stdin должен сразу давать EOF, а не висеть.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := Run(ctx, "", "/bin/sh", "-c", `read x; echo "eof:$?"`)
	if err != nil || got.Stdout != "eof:1\n" {
		t.Errorf("Run = %+v, %v; want Stdout %q", got, err, "eof:1\n")
	}
}

// Сигнал не от ctx — программа «завершилась»: Result с кодом −1, без ошибки.
func TestRunKilledBySignal(t *testing.T) {
	got, err := Run(context.Background(), "", "/bin/sh", "-c", `echo partial; kill -KILL $$`)
	if err != nil {
		t.Fatalf("Run(убит SIGKILL) вернул ошибку %v, want nil", err)
	}
	want := Result{Stdout: "partial\n", ExitCode: -1}
	if got != want {
		t.Errorf("Run(убит SIGKILL) = %+v, want %+v", got, want)
	}
}

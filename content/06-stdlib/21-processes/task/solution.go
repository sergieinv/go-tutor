package task

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Result — итог выполнения программы.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run запускает программу без оболочки и собирает её вывод.
func Run(ctx context.Context, stdin string, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	// Убитый по таймауту процесс даёт ExitError «signal: killed» —
	// настоящая причина в контексте.
	if err != nil && ctx.Err() != nil {
		return Result{}, fmt.Errorf("%s: %w", name, ctx.Err())
	}
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &exitErr):
		// Программа отработала и сообщила код — это не ошибка Run.
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	default:
		return Result{}, err // не запустилась: нет файла, нет прав
	}
}

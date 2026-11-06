package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// Main запускает run с контекстом, который отменяется по сигналу,
// и возвращает код выхода процесса.
func Main(run func(ctx context.Context) error, stderr io.Writer, sigs ...os.Signal) (code int) {
	if len(sigs) == 0 {
		sigs = []os.Signal{os.Interrupt, syscall.SIGTERM}
	}
	ctx, stop := signal.NotifyContext(context.Background(), sigs...)
	defer stop() // снять подписку: дальше сигналы снова работают по умолчанию

	// Паника в run не должна уронить процесс без кода и сообщения.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "panic: %v\n", r)
			code = 2
		}
	}()

	err := run(ctx)
	switch {
	case err == nil:
		return 0
	case ctx.Err() != nil && errors.Is(err, context.Canceled):
		return 0 // штатная остановка по сигналу
	default:
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
}

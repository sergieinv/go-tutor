package task

import (
	"context"
	"io"
	"os"
)

// Main запускает run с контекстом, который отменяется по сигналу,
// и возвращает код выхода процесса.
func Main(run func(ctx context.Context) error, stderr io.Writer, sigs ...os.Signal) int {
	// TODO: реализуйте функцию
	return 0
}

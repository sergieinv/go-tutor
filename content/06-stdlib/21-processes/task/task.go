package task

import "context"

// Result — итог выполнения программы.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run запускает программу без оболочки и собирает её вывод.
func Run(ctx context.Context, stdin string, name string, args ...string) (Result, error) {
	// TODO: реализуйте функцию
	return Result{}, nil
}

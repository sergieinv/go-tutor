package task

import "context"

// ProcessAll параллельно применяет f к items с помощью errgroup.
// Порядок результатов совпадает с items; при первой ошибке
// остальные вызовы отменяются, а результат — nil.
func ProcessAll(ctx context.Context, items []string, limit int,
	f func(ctx context.Context, item string) (int, error)) ([]int, error) {
	// TODO: реализуйте функцию
	return nil, nil
}

package task

import "context"

// Merge сливает каналы chans в один (fan-in). Выход закрывается,
// когда все входы закрыты, или после отмены ctx. nil-каналы
// пропускаются.
func Merge[T any](ctx context.Context, chans ...<-chan T) <-chan T {
	// TODO: реализуйте функцию
	return nil
}

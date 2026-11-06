// `context.Context` несёт сигнал отмены, дедлайн и значения
// запроса через всю цепочку вызовов. Это `CancellationToken`
// Go: его передают первым параметром каждой функции,
// которая может ждать.

package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Долгая операция слушает `ctx.Done()` в `select`: канал
// закрывается при отмене, и `ctx.Err()` объясняет почему.
func slowQuery(ctx context.Context, d time.Duration) (string, error) {
	select {
	case <-time.After(d):
		return "результат", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Ключ значения — свой неэкспортируемый тип: так ключи
// разных пакетов не столкнутся.
type ctxKey int

const requestIDKey ctxKey = 0

func main() {
	// Корень дерева — `Background()`: его нельзя отменить.
	// `WithTimeout` создаёт потомка, который отменится сам.
	// `cancel` вызываем всегда — он освобождает таймер.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := slowQuery(ctx, time.Second)
	fmt.Println("медленный запрос:", err)
	fmt.Println("это дедлайн:", errors.Is(err, context.DeadlineExceeded))

	// Отмена идёт вниз по дереву: отменили родителя —
	// отменены все потомки. Потомок родителя не трогает.
	parent, cancelParent := context.WithCancel(context.Background())
	child, cancelChild := context.WithCancel(parent)
	defer cancelChild()
	sibling, cancelSibling := context.WithCancel(parent)
	cancelSibling()
	fmt.Println("после отмены sibling:", parent.Err(), child.Err())
	cancelParent()
	fmt.Println("после отмены parent:", child.Err())

	// `WithValue` — для данных запроса, которые проходят
	// сквозь слои: id запроса, данные трассировки.
	reqCtx := context.WithValue(context.Background(), requestIDKey, "req-42")
	id, _ := reqCtx.Value(requestIDKey).(string)
	fmt.Println("request id:", id)

	// `WithCancelCause` позволяет указать причину отмены.
	// `Err()` по-прежнему `Canceled`, причину отдаёт `Cause`.
	causeCtx, cancelCause := context.WithCancelCause(context.Background())
	cancelCause(errors.New("клиент отключился"))
	fmt.Println(causeCtx.Err(), "/", context.Cause(causeCtx))

	// `AfterFunc` запускает функцию в своей горутине, когда
	// контекст отменён, — например, закрыть соединение.
	afCtx, afCancel := context.WithCancel(context.Background())
	closed := make(chan struct{})
	context.AfterFunc(afCtx, func() {
		fmt.Println("AfterFunc: закрываем соединение")
		close(closed)
	})
	afCancel()
	<-closed

	// `sibling` уже отменён — это видно и без `Err()`:
	// закрытый канал `Done()` готов к чтению сразу.
	select {
	case <-sibling.Done():
		fmt.Println("sibling.Done() закрыт")
	default:
		fmt.Println("sibling ещё жив")
	}
}

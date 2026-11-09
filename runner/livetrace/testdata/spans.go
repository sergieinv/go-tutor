// Цепочка из трёх «сервисов» в одном процессе: дедлайн запроса уменьшается
// на каждом шаге, третий сервис не укладывается и получает отмену.
package main

import (
	"context"
	"fmt"
	"runtime/trace"
	"time"
)

func left(ctx context.Context) string {
	d, _ := ctx.Deadline()
	return fmt.Sprintf("осталось %d мс", time.Until(d).Round(10*time.Millisecond).Milliseconds())
}

func call(ctx context.Context, name string, work time.Duration, next func(context.Context) error) error {
	ctx, task := trace.NewTask(ctx, name)
	defer task.End()
	trace.Log(ctx, "deadline", left(ctx))
	trace.WithRegion(ctx, "работа", func() {
		select {
		case <-time.After(work):
		case <-ctx.Done():
		}
	})
	if err := ctx.Err(); err != nil {
		trace.Log(ctx, "error", err.Error())
		return err
	}
	if next != nil {
		return next(ctx)
	}
	return nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	err := call(ctx, "gateway", 10*time.Millisecond, func(ctx context.Context) error {
		return call(ctx, "orders", 20*time.Millisecond, func(ctx context.Context) error {
			return call(ctx, "warehouse", 100*time.Millisecond, nil)
		})
	})
	fmt.Println(err)
}

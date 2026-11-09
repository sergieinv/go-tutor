// Живой визуализатор: четыре запроса за одним истёкшим ключом — сначала
// без защиты, потом через singleflight. Каждая волна — задача
// runtime/trace, каждый запрос — дочерняя задача, поход в «базу» —
// регион «БД», ожидание в singleflight — регион «singleflight.Do».
//
// Без защиты у всех четырёх запросов своя полоса «БД». С singleflight
// полоса «БД» одна — у того, кто начал загрузку; остальные ждут его
// результат. Уберите g.Do и вызовите load напрямую — и полос снова
// станет четыре.
package main

import (
	"context"
	"fmt"
	"runtime/trace"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

var dbCalls atomic.Int64

// load — медленный запрос в базу: 40 мс.
func load(ctx context.Context, sku string) string {
	dbCalls.Add(1)
	trace.WithRegion(ctx, "БД", func() { time.Sleep(40 * time.Millisecond) })
	return "карточка " + sku
}

func wave(name string, get func(ctx context.Context) string) {
	ctx, task := trace.NewTask(context.Background(), name)
	defer task.End()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			<-start // все четыре приходят одновременно: TTL только что истёк
			rctx, rtask := trace.NewTask(ctx, fmt.Sprintf("запрос %d", i+1))
			defer rtask.End()
			trace.Log(rctx, "кеш", "промах: "+get(rctx))
		})
	}
	close(start)
	wg.Wait()
}

func main() {
	wave("без защиты", func(ctx context.Context) string {
		return load(ctx, "apple")
	})
	fmt.Println("без защиты: загрузок из базы", dbCalls.Swap(0))

	var g singleflight.Group
	wave("singleflight", func(ctx context.Context) string {
		var v any
		var shared bool
		trace.WithRegion(ctx, "singleflight.Do", func() {
			v, _, shared = g.Do("apple", func() (any, error) {
				return load(ctx, "apple"), nil
			})
		})
		if shared {
			trace.Log(ctx, "singleflight", "результат общий")
		}
		return v.(string)
	})
	fmt.Println("singleflight: загрузок из базы", dbCalls.Load())
}

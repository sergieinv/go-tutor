// `errgroup` из `golang.org/x/sync` — это `WaitGroup`,
// который собирает ошибки: запускает задачи в горутинах,
// ждёт всех и возвращает первую ошибку. С контекстом
// он ещё и отменяет остальные задачи при первом сбое.

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// fetch имитирует запрос: ждёт, пока не пройдёт время или
// не отменят контекст. Адрес с "bad" — ошибка.
func fetch(ctx context.Context, url string, d time.Duration) (string, error) {
	if strings.Contains(url, "bad") {
		return "", fmt.Errorf("fetch %s: 500", url)
	}
	select {
	case <-time.After(d):
		return "<html>" + url, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func main() {
	urls := []string{"a.com", "b.com", "c.com"}

	// Нулевой `Group` готов к работе. `Go` запускает функцию
	// в горутине, `Wait` ждёт всех. Результаты — по индексу.
	var g errgroup.Group
	pages := make([]string, len(urls))
	for i, u := range urls {
		g.Go(func() error {
			p, err := fetch(context.Background(), u, 10*time.Millisecond)
			pages[i] = p
			return err
		})
	}
	if err := g.Wait(); err == nil {
		fmt.Println(pages)
	}

	// `WithContext` возвращает группу и контекст, который
	// отменяется при первой ошибке. Медленные задачи видят
	// отмену и выходят, не дожидаясь своей секунды.
	g2, ctx := errgroup.WithContext(context.Background())
	var canceled atomic.Int32
	for _, u := range []string{"a.com", "bad.com", "c.com"} {
		g2.Go(func() error {
			_, err := fetch(ctx, u, time.Second)
			if errors.Is(err, context.Canceled) {
				canceled.Add(1)
			}
			return err
		})
	}
	// `Wait` возвращает первую ошибку — настоящую причину,
	// а не `context.Canceled` от отменённых соседей.
	fmt.Println("ошибка:", g2.Wait())
	fmt.Println("отменено задач:", canceled.Load())
	fmt.Println("причина отмены:", context.Cause(ctx))

	// `SetLimit` ограничивает число одновременных горутин:
	// `Go` блокируется, пока не освободится место.
	var g3 errgroup.Group
	g3.SetLimit(2)
	var active atomic.Int32
	var overflow atomic.Bool
	for range 6 {
		g3.Go(func() error {
			if active.Add(1) > 2 {
				overflow.Store(true)
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			return nil
		})
	}
	g3.Wait()
	fmt.Println("больше двух одновременно:", overflow.Load())

	// `TryGo` не ждёт: если лимит исчерпан, возвращает false.
	var g4 errgroup.Group
	g4.SetLimit(1)
	release := make(chan struct{})
	g4.Go(func() error { <-release; return nil })
	fmt.Println("TryGo при занятом месте:", g4.TryGo(func() error { return nil }))
	close(release)
	g4.Wait()
}

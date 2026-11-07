package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// `t.Parallel()` ставит тест на паузу (`=== PAUSE`).
// Параллельные подтесты стартуют (`=== CONT`), когда
// функция родителя уже вернулась: `defer` родителя к этому
// моменту выполнен, а `t.Cleanup` ждёт подтесты. Сколько
// тестов идут одновременно, задаёт `-parallel` (по
// умолчанию GOMAXPROCS). Общие данные — только под
// защитой; гонки ловит `go test -race`.
func TestParallel(t *testing.T) {
	c := New(time.Minute)
	t.Cleanup(func() { t.Logf("cleanup родителя: записей %d", c.Len()) })
	defer t.Log("defer родителя: подтест ещё не запускался")

	t.Run("писатели", func(t *testing.T) {
		t.Parallel()
		var wg sync.WaitGroup
		for i := range 10 {
			wg.Go(func() { c.Set(fmt.Sprint(i), "v") })
		}
		wg.Wait()
	})
	t.Run("последовательный", func(t *testing.T) {
		t.Log("идёт сразу, пока параллельный на паузе")
	})
}

// `synctest.Test` (Go 1.25) запускает функцию в «пузыре»:
// там у `time` фейковые часы, они стартуют с 2000-01-01
// и идут вперёд, только когда все горутины пузыря
// заблокированы. Минута «сна» проходит мгновенно и точно.
func TestTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		c := New(time.Minute)
		c.Set("k", "v")

		time.Sleep(time.Minute - time.Nanosecond)
		if _, ok := c.Get("k"); !ok {
			t.Fatal("запись пропала раньше срока")
		}
		time.Sleep(time.Nanosecond)
		if _, ok := c.Get("k"); ok {
			t.Fatal("запись жива после TTL")
		}
		t.Logf("прошло %v, начало %s", time.Since(start), start.Format(time.DateOnly))
	})
}

// `synctest.Wait()` ждёт, пока все остальные горутины
// пузыря не заблокируются «прочно» (канал, `select`,
// `time.Sleep`, `sync.Cond`, `WaitGroup` из пузыря). Так
// тест узнаёт, что фоновая горутина обработала тик.
func TestJanitor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		c := New(time.Second)
		go c.Janitor(ctx, 10*time.Second)
		c.Set("a", "1")

		time.Sleep(10 * time.Second)
		synctest.Wait()
		t.Logf("через 10s записей: %d", c.Len())

		// До конца `synctest.Test` все горутины пузыря
		// должны завершиться — иначе тест упадёт.
		cancel()
	})
}

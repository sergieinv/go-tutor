// Сервис заказов за ограничителем нагрузки: две обработки одновременно,
// очередь до двух запросов, ждать в очереди не дольше 100 мс. Остальные
// получают отказ сразу — `503` с `Retry-After`, — а не висят до своего
// таймаута. Обработчик «застревает» на канале `gate`, поэтому вывод
// не зависит от скорости машины.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
)

// `semaphore.Weighted` ограничивает параллелизм и сам ведёт FIFO-очередь
// ждущих, но длину очереди не ограничивает. Поэтому рядом — счётчик
// `queued`: если очередь полна, запрос отбрасывается сразу.
type shedder struct {
	sem      *semaphore.Weighted
	maxQueue int64
	maxWait  time.Duration
	queued   atomic.Int64
	shed     atomic.Int64 // отказ сразу: очередь полна
	timedOut atomic.Int64 // отказ после ожидания в очереди
}

func (s *shedder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// `TryAcquire` не обгоняет очередь: при ждущих он вернёт false.
		if !s.sem.TryAcquire(1) {
			if s.queued.Add(1) > s.maxQueue {
				s.queued.Add(-1)
				s.shed.Add(1)
				overloaded(w, "очередь полна")
				return
			}
			// Ждём места не дольше `maxWait` и не дольше, чем ждёт клиент:
			// контекст запроса отменится, если клиент ушёл.
			ctx, cancel := context.WithTimeout(r.Context(), s.maxWait)
			err := s.sem.Acquire(ctx, 1)
			cancel()
			s.queued.Add(-1)
			if err != nil {
				s.timedOut.Add(1)
				overloaded(w, "не дождался места")
				return
			}
		}
		defer s.sem.Release(1)
		next.ServeHTTP(w, r)
	})
}

// Отказ из-за перегрузки — `503` и подсказка, когда приходить снова.
// Клиент с повторами (урок 12-microservices/05) должен её уважать.
func overloaded(w http.ResponseWriter, why string) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "перегружен: "+why, http.StatusServiceUnavailable)
}

func main() {
	var inside atomic.Int64 // сколько запросов дошло до обработчика
	gate := make(chan struct{})
	var gateMu sync.Mutex
	orders := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inside.Add(1)
		gateMu.Lock()
		g := gate
		gateMu.Unlock()
		<-g // «долгий» запрос в базу
		fmt.Fprint(w, "заказ создан")
	})
	lim := &shedder{sem: semaphore.NewWeighted(2), maxQueue: 2, maxWait: 100 * time.Millisecond}
	srv := httptest.NewUnstartedServer(lim.wrap(orders))
	srv.Config.ReadHeaderTimeout = time.Second
	srv.Start()
	defer srv.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	// Запросы отправляются по одному: следующий — только когда предыдущий
	// дошёл до обработчика, встал в очередь или получил отказ.
	wave := func(name string, n int, release func()) {
		fmt.Println(name)
		start := inside.Load() + lim.queued.Load() + lim.shed.Load() + lim.timedOut.Load()
		results := make([]string, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				resp, err := client.Post(srv.URL+"/orders", "text/plain", nil)
				if err != nil {
					results[i] = err.Error()
					return
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				results[i] = fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(string(body)))
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					results[i] += ", Retry-After: " + ra
				}
			})
			for inside.Load()+lim.queued.Load()+lim.shed.Load()+lim.timedOut.Load() < start+int64(i+1) {
				time.Sleep(time.Millisecond)
			}
		}
		release()
		wg.Wait()
		for i, r := range results {
			fmt.Printf("  запрос %d: %s\n", i+1, r)
		}
	}

	// Волна 1: двое работают, двое ждут, двое отброшены сразу. Потом
	// обработчики освобождаются, и очередь успевает получить места.
	wave("волна 1: 6 запросов, обработчики заняты", 6, func() { close(gate) })

	// Волна 2: обработчики заняты дольше, чем очередь готова ждать.
	// Ждущие уходят по таймауту очереди, а не по таймауту клиента.
	gateMu.Lock()
	gate = make(chan struct{})
	gateMu.Unlock()
	wave("волна 2: обработчики заняты дольше 100 мс", 4, func() {
		for lim.timedOut.Load() < 2 {
			time.Sleep(time.Millisecond)
		}
		close(gate)
	})

	fmt.Printf("итого: дошли до обработчика %d, отброшены сразу %d, по таймауту очереди %d\n",
		inside.Load(), lim.shed.Load(), lim.timedOut.Load())
}

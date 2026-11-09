// Сколько TCP-соединений открывает клиент под нагрузкой? Сервер
// на loopback считает новые соединения через `ConnState`, клиент —
// через `httptrace` (`GotConnInfo.Reused`). Выводим только числа,
// которые не зависят от машины: сценарии устроены так, что пул
// ведёт себя детерминированно.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Серверная сторона тоже настроена «под нагрузку»: таймаут
// на заголовки (защита от медленных клиентов), простой keep-alive
// не дольше `IdleTimeout` и лимит на размер заголовков.
// `ConnState(StateNew)` срабатывает на каждое принятое соединение.
func newServer(h http.Handler) (*httptest.Server, *atomic.Int64) {
	var conns atomic.Int64
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadHeaderTimeout = 2 * time.Second
	srv.Config.IdleTimeout = 30 * time.Second
	srv.Config.MaxHeaderBytes = 8 << 10
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	return srv, &conns
}

// `httptrace` вешает колбэки на этапы запроса. `GotConn` говорит,
// взято ли соединение из пула (`Reused`) или открыто заново.
func get(c *http.Client, url string, reused *atomic.Int64) (*http.Response, error) {
	trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) {
		if i.Reused {
			reused.Add(1)
		}
	}}
	ctx := httptrace.WithClientTrace(context.Background(), trace)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/small", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", 1<<20)) // 1 МБ
	})
	srv, conns := newServer(mux)
	defer srv.Close()

	// Десять запросов подряд, и каждый раз мы делаем по-разному
	// с телом ответа. `Transport` — новый на сценарий, чтобы пул
	// был пуст; в сервисе он один на процесс.
	scenario := func(name, path string, n int, handle func(*http.Response)) {
		c := &http.Client{Transport: &http.Transport{}, Timeout: 5 * time.Second}
		conns.Store(0)
		var reused atomic.Int64
		for range n {
			resp, err := get(c, srv.URL+path, &reused)
			if err != nil {
				fmt.Println("ошибка:", err)
				return
			}
			handle(resp)
		}
		fmt.Printf("%-28s новых: %2d, из пула: %d\n", name, conns.Load(), reused.Load())
	}

	// Правильно: дочитать до EOF и закрыть — соединение
	// возвращается в пул, и следующий запрос берёт его оттуда.
	scenario("дочитали и закрыли", "/small", 10, func(r *http.Response) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	})
	// Тело не закрыто: соединение навсегда занято этим ответом,
	// каждый запрос открывает новое. Это утечка, а не медленность.
	scenario("не закрыли", "/small", 10, func(r *http.Response) {})
	// Закрыли, не дочитав 1 МБ. Go 1.27 дочитывает при `Close` только
	// ответы до 256 КБ (по `Content-Length`), здесь больше — соединение
	// разрывается.
	scenario("закрыли, не дочитав 1 МБ", "/big", 10, func(r *http.Response) {
		r.Body.Close()
	})

	// Параллельная нагрузка: 20 запросов одновременно, три волны.
	// Сервер отвечает, только когда собрались все 20 запросов волны,
	// поэтому в каждой волне ровно 20 соединений заняты сразу.
	var mu sync.Mutex
	waiting, gate := 0, make(chan struct{})
	burstSrv, burstConns := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		g := gate
		if waiting++; waiting == 20 {
			waiting, gate = 0, make(chan struct{})
			close(g)
		}
		mu.Unlock()
		<-g
		io.WriteString(w, "ok")
	}))
	defer burstSrv.Close()

	// После волны в пуле остаётся только `MaxIdleConnsPerHost`
	// соединений, лишние закрываются. По умолчанию это 2 — и каждая
	// следующая волна открывает 18 соединений заново.
	for _, idle := range []int{0, 20} {
		c := &http.Client{
			Transport: &http.Transport{MaxIdleConnsPerHost: idle},
			Timeout:   5 * time.Second,
		}
		var waves []int64
		for range 3 {
			burstConns.Store(0)
			var wg sync.WaitGroup
			var reused atomic.Int64
			for range 20 {
				wg.Go(func() {
					resp, err := get(c, burstSrv.URL, &reused)
					if err != nil {
						return
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				})
			}
			wg.Wait()
			waves = append(waves, burstConns.Load())
		}
		fmt.Printf("MaxIdleConnsPerHost=%-2d новых по волнам: %v\n", idle, waves)
	}

	// `Client.Timeout` ограничивает весь запрос, включая чтение тела.
	// Ошибка — `net.Error` с `Timeout() == true`, и она же
	// находится через `errors.Is(err, context.DeadlineExceeded)`.
	release := make(chan struct{})
	slow, _ := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	c := &http.Client{Timeout: 100 * time.Millisecond}
	_, err := c.Get(slow.URL)
	var ne net.Error
	fmt.Println("таймаут клиента:", errors.As(err, &ne) && ne.Timeout(),
		errors.Is(err, context.DeadlineExceeded))
	close(release)
	slow.Close()

	// `MaxHeaderBytes` на сервере: заголовок в 64 КБ отвергается
	// ответом 431 ещё до обработчика.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/small", nil)
	req.Header.Set("X-Junk", strings.Repeat("a", 64<<10))
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		fmt.Println("ошибка:", err)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	fmt.Println("огромный заголовок:", resp.Status)
}

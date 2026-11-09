// Сервис под нагрузкой на loopback, и мы ищем его узкое место
// профилями. Программа сама пишет четыре профиля — CPU, allocs,
// mutex и block — и снимок `trace.FlightRecorder`. В песочнице
// у неё кнопка «pprof»: runner покажет `go tool pprof -top`
// по каждому `*.pprof`. Числа в профилях от запуска к запуску
// разные, а stdout печатает только то, что от машины не зависит.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	rpprof "runtime/pprof"
	"runtime/trace"
	"sync"
	"sync/atomic"
	"time"
)

// Сервис «заказов» с типичной ошибкой: общая статистика под
// мьютексом, и под тем же мьютексом — тяжёлая работа (хеш тела
// ответа). Под нагрузкой обработчики выстраиваются в очередь
// за блокировкой: CPU свободен, а запросы ждут.
type service struct {
	mu       sync.Mutex
	served   int
	fixed    atomic.Bool
	checksum uint64
}

func (s *service) handle(w http.ResponseWriter, r *http.Request) {
	body := fmt.Appendf(nil, `{"id":%q,"status":"new"}`, r.PathValue("id"))
	if s.fixed.Load() {
		// **Исправление:** тяжёлое — вне блокировки, под мьютексом
		// только короткое обновление общего состояния.
		sum := work(body)
		s.mu.Lock()
		s.served++
		s.checksum ^= sum
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		sum := work(body) // УЗКОЕ МЕСТО: работа под блокировкой
		s.served++
		s.checksum ^= sum
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func (s *service) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.served
}

// «Тяжёлая работа» запроса — перемешивание хеша тела ответа
// (здесь мог бы быть расчёт цены, проверка подписи, сжатие).
func work(b []byte) uint64 {
	h := uint64(14695981039346656037) // FNV-1a
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	for range 600_000 {
		h ^= h >> 29
		h *= 1099511628211
	}
	return h
}

// Метки профиля по эндпоинту: в `go tool pprof -tagfocus` можно
// оставить только сэмплы одного обработчика.
func labeled(name string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rpprof.Do(r.Context(), rpprof.Labels("handler", name), func(context.Context) { h(w, r) })
	}
}

// Нагрузка: 8 клиентов по 100 запросов. Возвращает число ошибок.
func load(c *http.Client, url string) (errs int) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range 100 {
				resp, err := c.Get(fmt.Sprintf("%s/orders/%d", url, w*1000+i))
				if err == nil {
					_, err = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				if err != nil || resp.StatusCode != http.StatusOK {
					mu.Lock()
					errs++
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()
	return errs
}

func main() {
	// Профили блокировок по умолчанию выключены: включаем на время
	// расследования. 1 — записывать каждое событие (в продакшне
	// берут выборку, например 100).
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)

	// **Flight recorder** пишет трассу в кольцевой буфер всё время;
	// на диск она попадает, только когда мы попросим.
	fr := trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: 2 * time.Second})
	if err := fr.Start(); err != nil {
		fmt.Println("flight recorder:", err)
		return
	}
	defer fr.Stop()

	// Публичный сервер — свой mux без `/debug/pprof/`.
	svc := &service{}
	public := http.NewServeMux()
	public.HandleFunc("GET /orders/{id}", labeled("orders", svc.handle))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen:", err)
		return
	}
	srv := &http.Server{Handler: public, ReadHeaderTimeout: 2 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	// **Служебный сервер** с `net/http/pprof` — отдельный mux
	// и только loopback. Снаружи его не видно.
	debug := http.NewServeMux()
	debug.HandleFunc("GET /debug/pprof/", pprof.Index)
	debug.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	debug.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	dln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen:", err)
		return
	}
	dsrv := &http.Server{Handler: debug, ReadHeaderTimeout: 2 * time.Second}
	go dsrv.Serve(dln)
	defer dsrv.Close()

	client := &http.Client{
		Transport: &http.Transport{MaxIdleConnsPerHost: 16},
		Timeout:   10 * time.Second,
	}
	base, debugURL := "http://"+ln.Addr().String(), "http://"+dln.Addr().String()

	// **CPU-профиль под нагрузкой** — оба прогона, до и после.
	cpu, err := os.Create("cpu.pprof")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer cpu.Close()
	if err := rpprof.StartCPUProfile(cpu); err != nil {
		fmt.Println(err)
		return
	}
	errs := load(client, base)
	fmt.Println("до исправления: запросов", svc.count(), "ошибок", errs)

	// Пока сервис работает, профиль можно снять по HTTP — так его
	// и забирают с живого сервера через проброс порта.
	resp, err := client.Get(debugURL + "/debug/pprof/goroutine?debug=1")
	if err == nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Println("служебный сервер отдал профиль горутин:", resp.StatusCode == 200 && len(b) > 0)
	}
	resp, err = client.Get(base + "/debug/pprof/")
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		fmt.Println("публичный сервер на /debug/pprof/:", resp.StatusCode)
	}

	// Исправляем и гоняем ту же нагрузку ещё раз. Сравните в
	// mutex-профиле и в `-top` CPU-профиля: ожидание блокировки
	// исчезает, хеширование идёт параллельно.
	svc.mu.Lock()
	svc.served = 0
	svc.mu.Unlock()
	svc.fixed.Store(true)
	errs = load(client, base)
	fmt.Println("после исправления: запросов", svc.count(), "ошибок", errs)
	rpprof.StopCPUProfile()

	// Остальные профили — накопленные с начала программы.
	for _, name := range []string{"allocs", "block", "mutex"} {
		f, err := os.Create(name + ".pprof")
		if err != nil {
			fmt.Println(err)
			return
		}
		rpprof.Lookup(name).WriteTo(f, 0)
		f.Close()
	}

	// Снимок последних секунд трассы — то, что сохраняют, когда
	// замечен медленный запрос. Смотрят его `go tool trace`.
	tf, err := os.Create("flight.trace")
	if err != nil {
		fmt.Println(err)
		return
	}
	n, err := fr.WriteTo(tf)
	tf.Close()
	fmt.Println("снимок flight recorder записан:", err == nil && n > 0)
	fmt.Println("записаны allocs.pprof, block.pprof, cpu.pprof, mutex.pprof")
}

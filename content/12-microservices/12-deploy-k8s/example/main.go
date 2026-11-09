// Сервис, готовый к Kubernetes: три пробы (startup, liveness, readiness),
// readiness зависит от базы, а liveness — нет, и остановка по SIGTERM
// в правильном порядке: сначала «не готов» (draining), потом пауза, чтобы
// балансировщик убрал под из ротации, потом Shutdown с ожиданием активных
// запросов. SIGTERM пример посылает сам себе — как kubelet при удалении пода.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Версия подставляется при сборке:
// go build -trimpath -ldflags "-s -w -X main.version=1.4.2" ./cmd/orders
var version = "dev"

// Стадии жизни пода. Переходы только вперёд: starting → ready → draining.
const (
	starting int32 = iota
	ready
	draining
)

var stageNames = [...]string{"starting", "ready", "draining"}

func main() {
	// Версия и путь модуля — в /version и в первой строке лога. Main.Version
	// из ReadBuildInfo заполняется из git-тега при go build в репозитории.
	if bi, ok := debug.ReadBuildInfo(); ok {
		fmt.Printf("сервис %s, версия %s\n", bi.Main.Path, version)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	var stage atomic.Int32
	var dbUp atomic.Bool
	dbUp.Store(true)

	mux := http.NewServeMux()
	// Liveness: «процесс жив и не завис». Никаких зависимостей — иначе
	// упавшая база перезапустит все поды разом, а базе это не поможет.
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	// Startup: пока старт не закончен (миграции, прогрев кеша), kubelet
	// не проверяет liveness и не убивает медленно стартующий под.
	mux.HandleFunc("GET /startupz", func(w http.ResponseWriter, r *http.Request) {
		if stage.Load() == starting {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	// Readiness: «можно слать трафик». Зависит от стадии и от того, без чего
	// сервис не может обслужить ни одного запроса, — здесь от базы.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		switch s := stage.Load(); {
		case s != ready:
			http.Error(w, stageNames[s], http.StatusServiceUnavailable)
		case !dbUp.Load():
			http.Error(w, "not ready: db", http.StatusServiceUnavailable) // без текста ошибки
		default:
			fmt.Fprintln(w, "ok")
		}
	})
	started, release := make(chan struct{}), make(chan struct{})
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release // запрос, который застанет остановку
		fmt.Fprintln(w, "created")
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	probe := func(when string) {
		var out []string
		for _, p := range []string{"startupz", "livez", "readyz"} {
			resp, err := client.Get(base + "/" + p)
			if err != nil {
				out = append(out, p+"=ошибка")
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			out = append(out, fmt.Sprintf("%s=%d", p, resp.StatusCode))
			if resp.StatusCode != http.StatusOK {
				out[len(out)-1] += fmt.Sprintf(" (%s)", strings.TrimSpace(string(body)))
			}
		}
		fmt.Printf("%-22s %s\n", when+":", strings.Join(out, "  "))
	}

	probe("старт")
	stage.Store(ready) // миграции применены, кеш прогрет
	probe("готов")
	dbUp.Store(false)
	probe("база недоступна")
	dbUp.Store(true)
	probe("база вернулась")

	// Запрос в работе, когда приходит SIGTERM.
	done := make(chan string, 1)
	go func() {
		resp, err := client.Post(base+"/orders", "text/plain", nil)
		if err != nil {
			done <- "ошибка: " + err.Error()
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		done <- strings.TrimSpace(string(body))
	}()
	<-started
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	<-ctx.Done()
	fmt.Println("получен SIGTERM")

	// 1. Перестать быть готовым: Service уберёт под из endpoints.
	stage.Store(draining)
	probe("draining")
	// 2. Пауза: kube-proxy и балансировщики узнают об этом не сразу,
	// и новые запросы ещё несколько секунд приходят. В проде здесь
	// time.Sleep(5 * time.Second) или preStop-хук; в примере — без паузы.
	// 3. Shutdown: закрыть listener и дождаться активных запросов —
	// не дольше terminationGracePeriodSeconds минус пауза.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	shut := make(chan error, 1)
	go func() { shut <- srv.Shutdown(shutdownCtx) }()
	close(release)
	fmt.Println("активный запрос:", <-done)
	fmt.Println("Shutdown:", <-shut)
	// 4. Только теперь закрыть базу, брокер, сбросить трассы и метрики.
	fmt.Println("зависимости закрыты, выход с кодом 0")
}

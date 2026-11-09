// Пакет app — сборка игрового сервера из частей. Вся композиция — в Run,
// явно и в порядке использования: без DI-контейнера граф объектов виден
// целиком, а порядок остановки — обратный порядку запуска. cmd/gameserver
// только читает окружение и вызывает Run; интеграционные тесты вызывают
// Run так же, с адресами 127.0.0.1:0.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"gotutor/labs/gameserver/internal/gateway"
	"gotutor/labs/gameserver/internal/mc"
	"gotutor/labs/gameserver/internal/metrics"
	"gotutor/labs/gameserver/internal/save"
	"gotutor/labs/gameserver/internal/world"
)

// Addrs — фактические адреса (при :0 порты выбирает система).
type Addrs struct{ MC, WS, Ops string }

const (
	drainTimeout = 5 * time.Second // соединениям — дописать kick и закрыться
	shutdownText = "Server is shutting down"
)

// Run запускает сервер и блокируется до отмены ctx. ready (если не nil)
// вызывается, когда все порты слушают.
func Run(ctx context.Context, cfg Config, log *slog.Logger, ready func(Addrs)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Метрики: свой реестр (не глобальный) + метрики рантайма Go и процесса.
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.New(reg)

	// Сохранение: читаем прошлый снапшот до создания мира.
	var store *save.Store
	var snap *world.Snapshot
	opts := world.Options{Log: log.With("component", "world"), Metrics: m}
	if cfg.SavePath != "" {
		store = save.New(cfg.SavePath, log.With("component", "save"), m)
		var err error
		if snap, err = store.Load(); err != nil {
			return err
		}
		opts.Snapshots = store
		go store.Run()
	}
	w := world.New(cfg.World, opts, snap)

	// Кодировщики протоколов: по одному на тип клиента.
	mcEnc := mc.NewEncoder(cfg.MC.CompressionThreshold)
	wsEnc := gateway.NewEncoder()
	reg.MustRegister(
		prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "gameserver_chunk_cache_hits_total", Help: "Minecraft chunk packets served from cache."},
			func() float64 { h, _ := mcEnc.ChunkCacheStats(); return float64(h) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "gameserver_chunk_cache_misses_total", Help: "Minecraft chunk packets encoded (and compressed)."},
			func() float64 { _, ms := mcEnc.ChunkCacheStats(); return float64(ms) }),
	)

	mcLn, err := net.Listen("tcp", cfg.MCAddr)
	if err != nil {
		return err
	}
	defer mcLn.Close()
	wsLn, err := net.Listen("tcp", cfg.WSAddr)
	if err != nil {
		return err
	}
	defer wsLn.Close()
	opsLn, err := net.Listen("tcp", cfg.OpsAddr)
	if err != nil {
		return err
	}
	defer opsLn.Close()

	mcSrv := mc.NewServer(cfg.MC, w, mcEnc, m, log.With("component", "minecraft"))
	gw := gateway.New(ctx, cfg.Gateway, w, wsEnc, m, log.With("component", "gateway"))
	// Таймауты HTTP: ReadHeaderTimeout — против медленных заголовков,
	// IdleTimeout — простаивающие keep-alive соединения (не /ws) не висят
	// вечно. Перехваченные WebSocket-соединения эти таймауты не трогают.
	wsSrv := &http.Server{
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Служебный порт: метрики, пробы, pprof. Наружу — только 127.0.0.1
	// (compose), pprof отдаёт стеки и профили — это не для всех.
	var stopping atomic.Bool
	ops := http.NewServeMux()
	ops.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	ops.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	ops.HandleFunc("GET /readyz", func(rw http.ResponseWriter, _ *http.Request) {
		if stopping.Load() {
			http.Error(rw, "shutting down", http.StatusServiceUnavailable)
			return
		}
		_, _ = rw.Write([]byte("ready\n"))
	})
	if cfg.Pprof {
		ops.HandleFunc("/debug/pprof/", pprof.Index)
		ops.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		ops.HandleFunc("/debug/pprof/profile", pprof.Profile)
		ops.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		ops.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	// WriteTimeout не ставим: /debug/pprof/profile пишет ответ десятки секунд.
	opsSrv := &http.Server{Handler: ops, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}

	log.Info("listening", "minecraft", mcLn.Addr().String(), "websocket", wsLn.Addr().String(), "ops", opsLn.Addr().String(),
		"view_distance", cfg.World.ViewDistance, "max_players", cfg.World.MaxPlayers,
		"compression", cfg.MC.CompressionThreshold, "save", cfg.SavePath, "origins", cfg.Gateway.Origins)

	// Запуск. Ошибка любого сервера останавливает всё.
	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	fail := func(err error) {
		if err != nil {
			errOnce.Do(func() { firstErr = err })
			cancel()
		}
	}
	finalSnap := make(chan *world.Snapshot, 1)
	go func() { finalSnap <- w.Run(ctx, shutdownText) }()
	wg.Add(3)
	mcServeDone := make(chan struct{})
	go func() { defer wg.Done(); defer close(mcServeDone); fail(mcSrv.Serve(ctx, mcLn)) }()
	go func() {
		defer wg.Done()
		if err := wsSrv.Serve(wsLn); !errors.Is(err, http.ErrServerClosed) {
			fail(err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := opsSrv.Serve(opsLn); !errors.Is(err, http.ErrServerClosed) {
			fail(err)
		}
	}()
	if ready != nil {
		ready(Addrs{MC: mcLn.Addr().String(), WS: wsLn.Addr().String(), Ops: opsLn.Addr().String()})
	}

	<-ctx.Done()
	// Остановка (SIGTERM от docker stop): readyz → 503, новые соединения не
	// принимаем, мир досчитывает тик, рассылает kick и отдаёт финальный
	// снапшот; соединения дописывают kick; снапшот пишется на диск.
	stopping.Store(true)
	log.Info("shutting down")
	shCtx, shCancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
	defer shCancel()
	// Shutdown закрывает слушатель и ждёт обработчики до перехвата
	// соединения; после перехвата их учитывает gw.Wait. Serve Minecraft
	// должен выйти до Wait: иначе wg.Add принятого в последний момент
	// соединения гонялся бы с Wait.
	_ = wsSrv.Shutdown(shCtx)
	<-mcServeDone
	final := <-finalSnap
	// Соединения дописывают kick параллельно — общий бюджет drainTimeout.
	var drain sync.WaitGroup
	drain.Add(2)
	go func() { defer drain.Done(); mcSrv.Wait(shCtx) }()
	go func() { defer drain.Done(); gw.Wait(shCtx) }()
	drain.Wait()
	if store != nil {
		store.Close() // дождаться фонового сохранения, если оно шло
		if err := store.Save(final); err != nil {
			log.Error("final save failed", "err", err)
			fail(err)
		}
	}
	_ = opsSrv.Shutdown(shCtx)
	wg.Wait()
	log.Info("stopped")
	return firstErr
}

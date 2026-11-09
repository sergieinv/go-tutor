// Package shed — отбрасывание нагрузки (load shedding) на дорогих вызовах
// (модуль 14).
//
// Без ограничения перегруженный сервис принимает всё: запросы копятся в
// очереди пула соединений базы и у склада, задержка растёт у всех, а
// клиенты отваливаются по дедлайну — сервис тратит ресурсы на ответы,
// которые уже никто не ждёт. Limiter держит не больше MaxInFlight
// одновременных вызовов и короткую очередь: если место не освободилось за
// QueueTimeout (или очередь полна), вызов сразу получает
// RESOURCE_EXHAUSTED с RetryInfo — через grpc-gateway это HTTP 429 с
// заголовком Retry-After. Быстрый отказ дешевле медленного таймаута: и для
// сервиса, и для клиента, который может повторить позже или в другую
// реплику.
//
// Лимит — статический (переменные окружения). Адаптивные лимиты (AIMD по
// задержке, как в Netflix concurrency-limits или gRPC xDS) честнее под
// меняющейся нагрузкой, но их поведение сложнее объяснить и проверить.
package shed

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Config — настройки ограничителя.
type Config struct {
	// MaxInFlight — сколько вызовов выполняется одновременно (> 0).
	MaxInFlight int
	// MaxQueue — сколько вызовов может ждать места; 0 — не ждать вовсе.
	MaxQueue int
	// QueueTimeout — сколько вызов ждёт места, прежде чем получить отказ.
	// Короткое: десятки–сотни миллисекунд, а не дедлайн клиента.
	QueueTimeout time.Duration
	// RetryAfter — подсказка клиенту в RetryInfo; 0 — 1 секунда.
	RetryAfter time.Duration
	// Registerer — куда зарегистрировать метрики; nil — без метрик.
	Registerer prometheus.Registerer
	Logger     *slog.Logger
	// NewTimer — фабрика таймеров ожидания; nil — time.NewTimer. Тесты
	// подставляют таймер, который срабатывает по команде.
	NewTimer func(d time.Duration) (c <-chan time.Time, stop func() bool)
}

// Limiter — семафор с ограниченной очередью ожидания.
type Limiter struct {
	slots   chan struct{}
	waiting atomic.Int64
	cfg     Config
	m       *metrics
}

// New создаёт ограничитель.
func New(cfg Config) *Limiter {
	if cfg.MaxInFlight <= 0 {
		panic("shed: MaxInFlight must be positive")
	}
	if cfg.RetryAfter == 0 {
		cfg.RetryAfter = time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.NewTimer == nil {
		cfg.NewTimer = func(d time.Duration) (<-chan time.Time, func() bool) {
			t := time.NewTimer(d)
			return t.C, t.Stop
		}
	}
	l := &Limiter{slots: make(chan struct{}, cfg.MaxInFlight), cfg: cfg}
	l.m = newMetrics(cfg.Registerer, l)
	return l
}

// Причины отказа (метка метрики orders_shed_rejected_total).
const (
	ReasonQueueFull = "queue_full" // очередь полна — отказ без ожидания
	ReasonTimeout   = "timeout"    // ждал QueueTimeout, место не освободилось
)

// Acquire занимает место. ok=false — отказ (reason — почему) или отмена
// ctx (тогда err != nil). При ok=true вызывающий обязан вызвать release.
func (l *Limiter) Acquire(ctx context.Context) (release func(), reason string, err error) {
	release = func() { <-l.slots }
	// Быстрый путь: место есть.
	select {
	case l.slots <- struct{}{}:
		return release, "", nil
	default:
	}
	if l.waiting.Add(1) > int64(l.cfg.MaxQueue) {
		l.waiting.Add(-1)
		return nil, ReasonQueueFull, nil
	}
	defer l.waiting.Add(-1)
	timeout, stop := l.cfg.NewTimer(l.cfg.QueueTimeout)
	defer stop()
	select {
	case l.slots <- struct{}{}:
		return release, "", nil
	case <-timeout:
		return nil, ReasonTimeout, nil
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
}

// InFlight — сколько вызовов выполняется сейчас.
func (l *Limiter) InFlight() int { return len(l.slots) }

// Waiting — сколько вызовов ждут места.
func (l *Limiter) Waiting() int { return int(l.waiting.Load()) }

// UnaryInterceptor ограничивает перечисленные методы (полные имена,
// /package.Service/Method); остальные проходят без ограничения. Чтение
// дешёвое и не должно страдать от очереди на создание заказов.
func (l *Limiter) UnaryInterceptor(methods ...string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !slices.Contains(methods, info.FullMethod) {
			return handler(ctx, req)
		}
		release, reason, err := l.Acquire(ctx)
		if err != nil {
			return nil, status.FromContextError(err).Err()
		}
		if release == nil {
			l.m.rejected.WithLabelValues(info.FullMethod, reason).Inc()
			return nil, l.overloaded()
		}
		defer release()
		return handler(ctx, req)
	}
}

// overloaded — RESOURCE_EXHAUSTED с RetryInfo: клиент понимает, что
// запрос не выполнялся вовсе (его можно повторить без риска дубля) и когда.
func (l *Limiter) overloaded() error {
	st := status.New(codes.ResourceExhausted, "server overloaded, retry later")
	if d, err := st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(l.cfg.RetryAfter)}); err == nil {
		st = d
	}
	return st.Err()
}

type metrics struct {
	rejected *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer, l *Limiter) *metrics {
	m := &metrics{rejected: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_shed_rejected_total",
		Help: "Calls rejected by load shedding with RESOURCE_EXHAUSTED, by method and reason (queue_full, timeout).",
	}, []string{"method", "reason"})}
	if reg != nil {
		reg.MustRegister(m.rejected,
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{
				Name: "orders_shed_in_flight",
				Help: "Limited calls executing now.",
			}, func() float64 { return float64(l.InFlight()) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{
				Name: "orders_shed_waiting",
				Help: "Limited calls waiting for a free slot.",
			}, func() float64 { return float64(l.Waiting()) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{
				Name: "orders_shed_max_in_flight",
				Help: "Configured concurrency limit.",
			}, func() float64 { return float64(l.cfg.MaxInFlight) }),
		)
	}
	return m
}

package consumer

import (
	"github.com/prometheus/client_golang/prometheus"

	"gotutor/labs/notifications/internal/notify"
)

// Результат обработки записи в метрике notifications_events_total: три
// исхода notify плюс отправка в DLQ.
const resultDLQ = "dlq"

// Причины отправки в DLQ (заголовок dlq-reason и метка метрики).
const (
	reasonDecode     = "decode"
	reasonProcessing = "processing"
)

// Metrics — метрики консьюмера.
type Metrics struct {
	events   *prometheus.CounterVec
	sent     *prometheus.CounterVec
	dlq      *prometheus.CounterVec
	retries  prometheus.Counter
	duration prometheus.Histogram
}

// NewMetrics регистрирует метрики в reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_events_total",
			Help: "Processed order events by event type and result.",
		}, []string{"type", "result"}),
		sent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_sent_total",
			Help: "Notifications sent by kind.",
		}, []string{"kind"}),
		dlq: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "notifications_dlq_total",
			Help: "Records sent to the dead letter topic by reason.",
		}, []string{"reason"}),
		retries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "notifications_processing_retries_total",
			Help: "Retries of failed event processing.",
		}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "notifications_event_processing_seconds",
			Help:    "Time to process one record, including retries and DLQ.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	reg.MustRegister(m.events, m.sent, m.dlq, m.retries, m.duration)

	// Ряды заводятся заранее с нулём: иначе ряд появляется только после
	// первого события, и rate() по нему на графике и в алерте
	// («DLQ растёт») сначала пуст, а не ноль.
	for _, t := range []string{notify.EventOrderCreated, notify.EventOrderCancelled} {
		for _, r := range []notify.Result{notify.ResultSent, notify.ResultDuplicate} {
			m.events.WithLabelValues(t, string(r))
		}
		m.events.WithLabelValues(t, resultDLQ)
	}
	m.events.WithLabelValues(notify.EventUnknown, string(notify.ResultSkipped))
	m.events.WithLabelValues(notify.EventUnknown, resultDLQ)
	for _, k := range []notify.Kind{notify.KindOrderCreated, notify.KindOrderCancelled} {
		m.sent.WithLabelValues(string(k))
	}
	for _, r := range []string{reasonDecode, reasonProcessing} {
		m.dlq.WithLabelValues(r)
	}
	return m
}

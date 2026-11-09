// Package replica — чтение истории заказов с реплики PostgreSQL с
// гарантией read-your-writes (модуль 14).
//
// История покупателя (ListOrders) — самый частый запрос, и ему не нужна
// основная база: его можно отдать потоковой реплике. Цена — задержка
// репликации: на стенде реплика нарочно применяет WAL на 200 мс позже
// (recovery_min_apply_delay). Покупатель, который только что оформил
// заказ и открыл историю, не должен его «потерять».
//
// Решение — токен согласованности на основе LSN (позиции в WAL):
//
//  1. После записи (CreateOrder, CancelOrder) сервис берёт
//     pg_current_wal_lsn() основной базы и отдаёт клиенту токен
//     «lsn:16/B374D848» (поле consistency_token ответа).
//  2. Клиент передаёт токен в ListOrders. Сервис идёт на реплику, только
//     если pg_last_wal_replay_lsn() реплики не меньше токена, иначе — на
//     основную базу. Без токена — на реплику (клиенту свежесть не важна).
//  3. Позиция реплики только растёт, поэтому последнее увиденное значение
//     кешируется: пока оно не меньше токена, лишний запрос к реплике не
//     нужен.
//
// Почему не «читать с основной N секунд после записи» (sticky по времени):
// задержка реплики не ограничена сверху — при нагрузке или после паузы
// она бывает и секундами, и окно пришлось бы угадывать. LSN проверяет сам
// факт: реплика применила запись или нет. И токен работает между
// репликами сервиса — ему не нужна память о клиенте на конкретном
// экземпляре.
//
// Ограничения: токен защищает только того, кто его передал (другой
// клиент/вкладка без токена может увидеть старую историю — это
// eventual consistency, и так задумано); реплика одна и только у шарда 0
// (у postgres-shard1 реплики нет — его покупатели читают с основной);
// недоступная реплика — чтение с основной (метрика reason="replica_error").
package replica

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"gotutor/labs/orders/internal/orders"
)

// Primary — основная база шарда: читает историю и знает позицию записи.
type Primary interface {
	ListOrders(ctx context.Context, customerID, beforeID int64, limit int) ([]orders.Order, error)
	WALPosition(ctx context.Context) (uint64, error)
}

// Replica — реплика: читает историю и знает, что уже применила.
type Replica interface {
	ListOrders(ctx context.Context, customerID, beforeID int64, limit int) ([]orders.Order, error)
	ReplayPosition(ctx context.Context) (uint64, error)
}

// Config — настройки.
type Config struct {
	// Primaries — основные базы шардов по номерам.
	Primaries []Primary
	// Replica — реплика шарда 0; nil — реплики нет, всё с основных.
	Replica Replica
	// ShardOf — номер шарда покупателя; nil — всегда 0.
	ShardOf func(customerID int64) int
	// Enabled — выключатель чтения с реплики; nil — всегда включено.
	Enabled func() bool
	// Registerer — куда зарегистрировать метрики; nil — без метрик.
	Registerer prometheus.Registerer
	Logger     *slog.Logger
}

// History реализует orders.History.
type History struct {
	cfg Config
	// replayed — последняя увиденная позиция реплики (растёт монотонно).
	replayed atomic.Uint64
	reads    *prometheus.CounterVec

	lastLog atomic.Int64 // unix nano последней записи об ошибке реплики
}

var _ orders.History = (*History)(nil)

// New создаёт History.
func New(cfg Config) (*History, error) {
	if len(cfg.Primaries) == 0 {
		return nil, errors.New("replica: no primaries")
	}
	if cfg.ShardOf == nil {
		cfg.ShardOf = func(int64) int { return 0 }
	}
	if cfg.Enabled == nil {
		cfg.Enabled = func() bool { return true }
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	h := &History{cfg: cfg, reads: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_history_reads_total",
		Help: "Customer history reads by target (replica, primary) and reason: no_token/caught_up (replica), " +
			"lagging, no_replica, disabled, forced, replica_error (primary).",
	}, []string{"target", "reason"})}
	if cfg.Registerer != nil {
		cfg.Registerer.MustRegister(h.reads)
	}
	return h, nil
}

// Токены. Формат — деталь реализации: для клиента токен непрозрачен.
const (
	tokenLSN     = "lsn:"
	tokenPrimary = "primary" // позицию узнать не удалось — читать с основной
)

// Причины выбора (метка reason).
const (
	reasonNoToken      = "no_token"
	reasonCaughtUp     = "caught_up"
	reasonLagging      = "lagging"
	reasonNoReplica    = "no_replica"
	reasonDisabled     = "disabled"
	reasonForced       = "forced"
	reasonReplicaError = "replica_error"
)

// WritePosition — токен после записи покупателя. Пустой, если у его шарда
// нет реплики: читать оттуда всё равно будут с основной.
func (h *History) WritePosition(ctx context.Context, customerID int64) orders.Position {
	i := h.cfg.ShardOf(customerID)
	if i != 0 || h.cfg.Replica == nil {
		return ""
	}
	lsn, err := h.cfg.Primaries[0].WALPosition(ctx)
	if err != nil {
		// Без позиции безопасный токен один — «читать с основной».
		h.cfg.Logger.WarnContext(ctx, "wal position unavailable, token forces primary reads", "err", err)
		return tokenPrimary
	}
	return orders.Position(tokenLSN + formatLSN(lsn))
}

// ListOrders читает историю с реплики, если она достаточно свежая.
func (h *History) ListOrders(ctx context.Context, customerID, beforeID int64, limit int, after orders.Position) ([]orders.Order, error) {
	need, force, err := parseToken(after)
	if err != nil {
		return nil, &orders.ValidationError{Field: "consistency_token", Message: "invalid consistency token"}
	}
	i := h.cfg.ShardOf(customerID)
	primary := h.cfg.Primaries[i]
	switch {
	case i != 0 || h.cfg.Replica == nil:
		return h.primary(ctx, primary, reasonNoReplica, customerID, beforeID, limit)
	case !h.cfg.Enabled():
		return h.primary(ctx, primary, reasonDisabled, customerID, beforeID, limit)
	case force:
		return h.primary(ctx, primary, reasonForced, customerID, beforeID, limit)
	}

	reason := reasonNoToken
	if need > 0 {
		reason = reasonCaughtUp
		if h.replayed.Load() < need {
			pos, err := h.cfg.Replica.ReplayPosition(ctx)
			if err != nil {
				h.logReplicaError(ctx, err)
				return h.primary(ctx, primary, reasonReplicaError, customerID, beforeID, limit)
			}
			h.observe(pos)
			if pos < need {
				return h.primary(ctx, primary, reasonLagging, customerID, beforeID, limit)
			}
		}
	}
	list, err := h.cfg.Replica.ListOrders(ctx, customerID, beforeID, limit)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		h.logReplicaError(ctx, err)
		return h.primary(ctx, primary, reasonReplicaError, customerID, beforeID, limit)
	}
	h.reads.WithLabelValues("replica", reason).Inc()
	return list, nil
}

func (h *History) primary(ctx context.Context, p Primary, reason string, customerID, beforeID int64, limit int) ([]orders.Order, error) {
	h.reads.WithLabelValues("primary", reason).Inc()
	return p.ListOrders(ctx, customerID, beforeID, limit)
}

// observe поднимает кешированную позицию реплики (только вверх: два
// запроса могли прочитать позиции в разном порядке).
func (h *History) observe(pos uint64) {
	for {
		cur := h.replayed.Load()
		if pos <= cur || h.replayed.CompareAndSwap(cur, pos) {
			return
		}
	}
}

// logReplicaError пишет в журнал не чаще раза в 10 секунд.
func (h *History) logReplicaError(ctx context.Context, err error) {
	now := time.Now().UnixNano()
	last := h.lastLog.Load()
	if now-last < int64(10*time.Second) || !h.lastLog.CompareAndSwap(last, now) {
		return
	}
	h.cfg.Logger.WarnContext(ctx, "replica read failed, reading from primary", "err", err)
}

// parseToken: "" — без требований; "primary" — только основная;
// "lsn:X/Y" — не старее позиции.
func parseToken(p orders.Position) (need uint64, force bool, err error) {
	s := string(p)
	switch {
	case s == "":
		return 0, false, nil
	case s == tokenPrimary:
		return 0, true, nil
	case strings.HasPrefix(s, tokenLSN):
		lsn, err := parseLSN(strings.TrimPrefix(s, tokenLSN))
		return lsn, false, err
	}
	return 0, false, fmt.Errorf("unknown token %q", s)
}

// parseLSN и formatLSN — текстовая позиция WAL «16/B374D848». Своя копия,
// а не postgres.ParseLSN: пакет не зависит от драйвера.
func parseLSN(s string) (uint64, error) {
	hi, lo, ok := strings.Cut(s, "/")
	if !ok {
		return 0, fmt.Errorf("malformed lsn %q", s)
	}
	h, err1 := strconv.ParseUint(hi, 16, 32)
	l, err2 := strconv.ParseUint(lo, 16, 32)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("malformed lsn %q", s)
	}
	return h<<32 | l, nil
}

func formatLSN(lsn uint64) string { return fmt.Sprintf("%X/%X", lsn>>32, uint32(lsn)) }

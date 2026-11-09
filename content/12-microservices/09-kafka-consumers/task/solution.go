package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Handler обрабатывает одну запись; ошибка — запись не обработана.
type Handler func(ctx context.Context, r *kgo.Record) error

// Run — цикл потребителя группы с ручным коммитом после обработки.
// Клиент создан с kgo.DisableAutoCommit(); Run его не закрывает.
func Run(ctx context.Context, cl *kgo.Client, h Handler) error {
	for {
		fetches := cl.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}
		if err := fetches.Err(); err != nil {
			return fmt.Errorf("опрос: %w", err)
		}
		var done []*kgo.Record
		var herr error
		stopped := false
		for r := range fetches.RecordsAll() {
			if ctx.Err() != nil {
				stopped = true
				break
			}
			if err := h(ctx, r); err != nil {
				herr = fmt.Errorf("%s/%d@%d: %w", r.Topic, r.Partition, r.Offset, err)
				break
			}
			done = append(done, r)
		}
		if err := commit(ctx, cl, done); err != nil {
			return errors.Join(herr, err)
		}
		if herr != nil {
			return herr
		}
		if stopped {
			return nil
		}
	}
}

// commit коммитит обработанное даже после отмены ctx: остановка
// сервиса не должна терять прогресс.
func commit(ctx context.Context, cl *kgo.Client, done []*kgo.Record) error {
	if len(done) == 0 {
		return nil
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := cl.CommitRecords(cctx, done...); err != nil {
		return fmt.Errorf("коммит: %w", err)
	}
	return nil
}

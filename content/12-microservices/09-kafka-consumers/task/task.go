package task

import (
	"context"
	"errors"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Handler обрабатывает одну запись; ошибка — запись не обработана.
type Handler func(ctx context.Context, r *kgo.Record) error

// Run — цикл потребителя группы с ручным коммитом после обработки.
// Клиент создан с kgo.DisableAutoCommit(); Run его не закрывает.
func Run(ctx context.Context, cl *kgo.Client, h Handler) error {
	// TODO
	return errors.New("не реализовано")
}

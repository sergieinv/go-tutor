package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Valkey — KV на valkey-go.
type Valkey struct {
	client valkey.Client
}

var _ KV = (*Valkey)(nil)

// NewValkey подключается к Valkey по адресу host:port.
//
// Недоступный при старте Valkey — не ошибка старта: кеш необязателен.
// ForceSingleClient возвращает клиента, даже если первое соединение не
// удалось, и клиент переподключается сам при следующих командах; до тех
// пор операции возвращают ошибки, и кеш читает из базы. Неудачное первое
// соединение пишется в журнал log.
func NewValkey(addr string, log *slog.Logger) (*Valkey, error) {
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
		// Клиентский кеш (CLIENT TRACKING) не нужен: свой кеш и есть кеш.
		DisableCache: true,
		// Один узел, без кластера и sentinel: не тратить время на CLUSTER SLOTS.
		ForceSingleClient: true,
		// Повторы внутри клиента не нужны: таймаут операции короткий, а
		// промах кеша дешевле ожидания.
		DisableRetry: true,
		Dialer:       net.Dialer{Timeout: time.Second},
	})
	if client == nil {
		return nil, fmt.Errorf("valkey client: %w", err)
	}
	if err != nil {
		log.Warn("valkey unavailable, catalog reads go to the db until it is back", "addr", addr, "err", err)
	}
	return &Valkey{client: client}, nil
}

// MGet читает ключи одной командой.
func (v *Valkey) MGet(ctx context.Context, keys []string) ([]*string, error) {
	msgs, err := v.client.Do(ctx, v.client.B().Mget().Key(keys...).Build()).ToArray()
	if err != nil {
		return nil, err
	}
	out := make([]*string, len(msgs))
	for i := range msgs {
		s, err := msgs[i].ToString()
		if valkey.IsValkeyNil(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[i] = &s
	}
	return out, nil
}

// Set записывает значение с TTL (SET key value PX ms).
func (v *Valkey) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return v.client.Do(ctx, v.client.B().Set().Key(key).Value(value).Px(ttl).Build()).Error()
}

// Close закрывает соединения.
func (v *Valkey) Close() { v.client.Close() }

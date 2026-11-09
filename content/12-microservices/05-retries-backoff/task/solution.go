package task

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Retryable сообщает, стоит ли повторять ошибку: только временные отказы.
func Retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.Aborted:
		return true
	}
	return false
}

// RetryDelay достаёт подсказку сервера из детали RetryInfo.
func RetryDelay(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	st, ok := status.FromError(err)
	if !ok {
		return 0, false
	}
	for _, d := range st.Details() {
		ri, ok := d.(*errdetails.RetryInfo)
		if !ok || ri.GetRetryDelay() == nil || !ri.GetRetryDelay().IsValid() {
			continue
		}
		if delay := ri.GetRetryDelay().AsDuration(); delay >= 0 {
			return delay, true
		}
	}
	return 0, false
}

// Budget — ведро токенов: повторы разрешены, пока неудач немного.
type Budget struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	ratio  float64
}

func NewBudget(maxTokens, ratio float64) *Budget {
	return &Budget{tokens: maxTokens, max: maxTokens, ratio: ratio}
}

func (b *Budget) OnSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.max, b.tokens+b.ratio)
}

func (b *Budget) OnFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = max(0, b.tokens-1)
}

func (b *Budget) AllowRetry() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tokens > b.max/2
}

func (b *Budget) Tokens() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tokens
}

// Policy — политика повторов с внедрёнными часами, сном и случайностью.
type Policy struct {
	MaxAttempts int
	Base, Max   time.Duration
	Rand        func() float64
	Now         func() time.Time
	Sleep       func(ctx context.Context, d time.Duration) error
	Budget      *Budget
}

// Backoff — полный джиттер: Rand() × min(Base·2^(k−1), Max).
func (p Policy) Backoff(attempt int) time.Duration {
	limit := p.Base
	// Удваиваем, пока не дошли до потолка: так Duration не переполнится.
	for i := 1; i < attempt && limit < p.Max; i++ {
		limit *= 2
	}
	limit = min(limit, p.Max)
	rnd := p.Rand
	if rnd == nil {
		rnd = rand.Float64
	}
	return time.Duration(rnd() * float64(limit))
}

func (p Policy) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	attempts := max(p.MaxAttempts, 1)
	now := p.Now
	if now == nil {
		now = time.Now
	}
	sleep := p.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			if p.Budget != nil {
				p.Budget.OnSuccess()
			}
			return nil
		}
		if !Retryable(err) {
			return err
		}
		if p.Budget != nil {
			p.Budget.OnFailure()
		}
		if attempt >= attempts || (p.Budget != nil && !p.Budget.AllowRetry()) {
			return err
		}
		d, ok := RetryDelay(err)
		if !ok {
			d = p.Backoff(attempt)
		}
		if dl, ok := ctx.Deadline(); ok && now().Add(d).After(dl) {
			return err
		}
		if serr := sleep(ctx, d); serr != nil {
			return serr
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

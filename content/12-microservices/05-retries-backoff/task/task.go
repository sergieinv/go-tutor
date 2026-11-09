package task

import (
	"context"
	"time"
)

// Retryable сообщает, стоит ли повторять ошибку: Unavailable,
// ResourceExhausted, Aborted (в том числе обёрнутые через %w).
func Retryable(err error) bool {
	// TODO
	return false
}

// RetryDelay достаёт паузу из детали *errdetails.RetryInfo статуса ошибки.
func RetryDelay(err error) (time.Duration, bool) {
	// TODO
	return 0, false
}

// Budget — бюджет повторов (ведро токенов), безопасный для горутин.
type Budget struct {
	// TODO
}

func NewBudget(maxTokens, ratio float64) *Budget {
	// TODO
	return &Budget{}
}

func (b *Budget) OnSuccess() {
	// TODO
}

func (b *Budget) OnFailure() {
	// TODO
}

func (b *Budget) AllowRetry() bool {
	// TODO
	return false
}

func (b *Budget) Tokens() float64 {
	// TODO
	return 0
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
	// TODO
	return 0
}

// Do вызывает fn с повторами по правилам из условия.
func (p Policy) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	// TODO
	return fn(ctx)
}

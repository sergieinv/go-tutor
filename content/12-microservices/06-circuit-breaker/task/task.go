package task

import (
	"errors"
	"time"
)

// State — состояние circuit breaker.
type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	// TODO: "closed", "open", "half-open"
	return ""
}

// ErrOpen — отказ без вызова: breaker разомкнут или пробы заняты.
var ErrOpen = errors.New("circuit breaker is open")

// Config — пороги и внедряемые зависимости.
type Config struct {
	FailureThreshold int
	OpenTimeout      time.Duration
	HalfOpenProbes   int
	IsFailure        func(error) bool
	Now              func() time.Time
}

// Breaker — circuit breaker по числу неудач подряд.
type Breaker struct {
	// TODO
}

func New(cfg Config) *Breaker {
	// TODO
	return &Breaker{}
}

func (b *Breaker) State() State {
	// TODO
	return Closed
}

func (b *Breaker) Do(fn func() error) error {
	// TODO
	return fn()
}

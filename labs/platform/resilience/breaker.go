package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrOpen — выключатель разомкнут: вызов не делался. Это не ошибка соседа,
// поэтому Retry её не повторяет (Retryable должен возвращать для неё false).
var ErrOpen = errors.New("circuit breaker is open")

// State — состояние выключателя.
type State int

const (
	// StateClosed — вызовы идут, неудачи подряд считаются.
	StateClosed State = iota
	// StateOpen — вызовы сразу отклоняются с ErrOpen, соседу дают отдохнуть.
	StateOpen
	// StateHalfOpen — пропускается один пробный вызов: успех замыкает
	// выключатель, неудача снова размыкает.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	}
	return "unknown"
}

// BreakerConfig — настройки выключателя.
type BreakerConfig struct {
	// FailureThreshold — сколько неудач подряд размыкают выключатель.
	FailureThreshold int
	// OpenTimeout — сколько выключатель разомкнут до пробного вызова.
	OpenTimeout time.Duration
	// IsFailure решает, считать ли ошибку неудачей соседа. Ошибки клиента
	// (InvalidArgument, NotFound, «нет товара») и отмена вызывающим соседа
	// не характеризуют и не должны размыкать выключатель. nil — любая ошибка.
	IsFailure func(error) bool
	// Now — часы. nil — time.Now.
	Now func() time.Time
	// OnStateChange вызывается после смены состояния, вне блокировки
	// (метрика, журнал). Может быть nil.
	OnStateChange func(from, to State)
}

// Breaker — автоматический выключатель по числу неудач подряд.
//
// Зачем: если сосед лёг, каждый вызов к нему ждёт таймаута, держит
// горутину и соединение, а повторы добивают соседа, мешая ему подняться.
// Разомкнутый выключатель отвечает мгновенно, и вызывающий сразу
// возвращает понятную ошибку («склад недоступен»).
//
// Безопасен для конкурентного использования.
type Breaker struct {
	cfg BreakerConfig

	mu       sync.Mutex
	state    State
	failures int       // неудач подряд в StateClosed
	openedAt time.Time // когда разомкнулся
	probing  bool      // в StateHalfOpen пробный вызов уже идёт
}

// NewBreaker создаёт замкнутый выключатель.
func NewBreaker(cfg BreakerConfig) *Breaker {
	if cfg.FailureThreshold < 1 {
		cfg.FailureThreshold = 1
	}
	return &Breaker{cfg: cfg}
}

// State возвращает текущее состояние (для метрик). Разомкнутый выключатель,
// у которого истёк OpenTimeout, остаётся StateOpen до следующего вызова.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Do выполняет fn, если выключатель её пропускает, и учитывает результат.
// Отклонённый вызов возвращает ErrOpen.
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := b.allow(); err != nil {
		return err
	}
	// record — в defer: паника в fn не должна оставить пробный вызов
	// «идущим» навсегда (тогда выключатель отклонял бы всё).
	err := errPanicked
	defer func() { b.record(err) }()
	err = fn(ctx)
	return err
}

// errPanicked — исход вызова, который не вернулся из-за паники.
var errPanicked = errors.New("call panicked")

func (b *Breaker) allow() error {
	b.mu.Lock()
	from := b.state
	switch b.state {
	case StateOpen:
		if b.now().Sub(b.openedAt) < b.cfg.OpenTimeout {
			b.mu.Unlock()
			return ErrOpen
		}
		b.state, b.probing = StateHalfOpen, true
	case StateHalfOpen:
		if b.probing {
			// Один пробный вызов за раз: остальные ждут его исхода.
			b.mu.Unlock()
			return ErrOpen
		}
		b.probing = true
	}
	to := b.state
	b.mu.Unlock()
	b.notify(from, to)
	return nil
}

func (b *Breaker) record(err error) {
	failure := err != nil && (b.cfg.IsFailure == nil || b.cfg.IsFailure(err))

	b.mu.Lock()
	from := b.state
	switch b.state {
	case StateClosed:
		if !failure {
			b.failures = 0
			break
		}
		b.failures++
		if b.failures >= b.cfg.FailureThreshold {
			b.open()
		}
	case StateHalfOpen:
		b.probing = false
		if failure {
			b.open()
		} else {
			b.state, b.failures = StateClosed, 0
		}
	case StateOpen:
		// Результат вызова, начатого до размыкания: состояние уже решено.
	}
	to := b.state
	b.mu.Unlock()
	b.notify(from, to)
}

// open размыкает выключатель. Вызывается под b.mu.
func (b *Breaker) open() {
	b.state, b.openedAt, b.failures = StateOpen, b.now(), 0
}

func (b *Breaker) notify(from, to State) {
	if from != to && b.cfg.OnStateChange != nil {
		b.cfg.OnStateChange(from, to)
	}
}

func (b *Breaker) now() time.Time {
	if b.cfg.Now != nil {
		return b.cfg.Now()
	}
	return time.Now()
}

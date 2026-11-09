// Package resilience — повторы с экспоненциальной паузой и автоматический
// выключатель (circuit breaker) для вызовов соседних сервисов.
//
// В .NET это Polly или Microsoft.Extensions.Http.Resilience. Здесь — две
// небольшие структуры без зависимостей: время (часы и сон) внедряется, и
// тесты проверяют поведение детерминированно, без настоящих пауз.
package resilience

import (
	"context"
	"math/rand/v2"
	"time"
)

// Retry повторяет операцию, пока она завершается временной ошибкой.
//
// Повторять можно только идемпотентные операции: повтор неидемпотентной
// (списать деньги без ключа идемпотентности) превращает таймаут в двойное
// списание. Повтор — не бесплатен: при массовом сбое каждый клиент
// умножает нагрузку на больного соседа (retry storm), поэтому попыток мало,
// паузы растут экспоненциально и случайны (full jitter), а дедлайн
// вызывающего важнее числа попыток.
type Retry struct {
	// MaxAttempts — всего попыток, включая первую. 0 или 1 — без повторов.
	MaxAttempts int
	// BaseDelay — верхняя граница первой паузы; дальше удваивается.
	BaseDelay time.Duration
	// MaxDelay ограничивает верхнюю границу паузы.
	MaxDelay time.Duration
	// Retryable решает, временная ли ошибка. nil — повторов нет.
	Retryable func(error) bool

	// Sleep ждёт d или отмены ctx. nil — настоящий таймер.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now — текущее время для проверки дедлайна. nil — time.Now.
	Now func() time.Time
	// Jitter возвращает случайную паузу из [0, max]. nil — math/rand/v2.
	Jitter func(max time.Duration) time.Duration
	// OnRetry вызывается перед каждой паузой (метрики, журнал). Может быть nil.
	OnRetry func(attempt int, delay time.Duration, err error)
}

// Do вызывает fn до успеха, нефатальной ошибки или исчерпания попыток и
// возвращает результат последней попытки. Если пауза не помещается в
// дедлайн ctx, повтор не делается: ответ «не успели» лучше раньше.
func (r Retry) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	for attempt := 1; ; attempt++ {
		err := fn(ctx)
		if err == nil || attempt >= r.MaxAttempts || r.Retryable == nil || !r.Retryable(err) || ctx.Err() != nil {
			return err
		}
		delay := r.jitter(r.backoff(attempt))
		if deadline, ok := ctx.Deadline(); ok && r.now().Add(delay).After(deadline) {
			return err
		}
		if r.OnRetry != nil {
			r.OnRetry(attempt, delay, err)
		}
		if r.sleep(ctx, delay) != nil {
			// Контекст отменён во время паузы: возвращаем ошибку операции —
			// она объясняет, почему вообще понадобился повтор.
			return err
		}
	}
}

// backoff — верхняя граница паузы перед повтором после попытки attempt:
// BaseDelay·2^(attempt−1), но не больше MaxDelay.
func (r Retry) backoff(attempt int) time.Duration {
	d := r.BaseDelay
	for i := 1; i < attempt && d < r.MaxDelay; i++ {
		d *= 2
	}
	return min(d, r.MaxDelay)
}

func (r Retry) jitter(max time.Duration) time.Duration {
	if r.Jitter != nil {
		return r.Jitter(max)
	}
	if max <= 0 {
		return 0
	}
	// Full jitter: равномерно на [0, max]. Клиенты, упавшие одновременно,
	// не придут повторять тоже одновременно.
	return rand.N(max + 1)
}

func (r Retry) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r Retry) sleep(ctx context.Context, d time.Duration) error {
	if r.Sleep != nil {
		return r.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

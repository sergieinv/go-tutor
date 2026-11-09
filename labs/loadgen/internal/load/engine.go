// Package load — генератор нагрузки: модели прихода запросов, сценарии
// REST-клиента «Заказов», сбор задержек и отчёт.
//
// Две модели:
//
//   - Открытая (RunOpen): запросы приходят с заданной частотой независимо
//     от того, успевает ли сервис, — как реальные пользователи из
//     интернета. Расписание фиксировано заранее (i-й сценарий — в момент
//     start + i/rate), а задержка считается от запланированного момента.
//     Если сервис тормозит, расписание не ждёт: новые запросы идут по
//     плану, а время, которое запрос «простоял» до отправки, входит в его
//     задержку. Так нет coordinated omission — ошибки, из-за которой
//     генератор, ждущий ответа перед следующим запросом, сам снижает
//     нагрузку именно тогда, когда сервис плох, и рисует красивые
//     перцентили.
//   - Закрытая (RunClosed): N виртуальных пользователей, каждый делает
//     сценарий, ждёт think time и повторяет. Частота сама подстраивается
//     под скорость сервиса — так ведут себя пулы воркеров и батч-клиенты.
//     Задержка — от фактической отправки; закрытая модель по своей природе
//     недооценивает хвост при деградации, это её свойство, а не баг.
package load

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// OpenConfig — открытая модель.
type OpenConfig struct {
	// Rate — сценариев в секунду (> 0).
	Rate float64
	// Duration — сколько длится расписание.
	Duration time.Duration
	// MaxInFlight — предохранитель генератора: больше стольких сценариев
	// одновременно не запускать (иначе при «мёртвом» сервисе генератор
	// наплодит горутин без ограничения). Пропущенные считаются в
	// Stats.Dropped. 0 — 10000.
	MaxInFlight int
	// Seed — зерно случайности (выбор сценария, покупателя, товара).
	Seed uint64
}

// RunOpen выполняет открытую модель и заполняет stats.
func RunOpen(ctx context.Context, c *Client, mix []Scenario, cfg OpenConfig, stats *Stats) {
	if cfg.MaxInFlight == 0 {
		cfg.MaxInFlight = 10000
	}
	sem := make(chan struct{}, cfg.MaxInFlight)
	picker := rand.New(rand.NewPCG(cfg.Seed, 0))
	interval := float64(time.Second) / cfg.Rate
	var wg sync.WaitGroup
	start := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C
	for i := 0; ; i++ {
		intended := start.Add(time.Duration(float64(i) * interval))
		if intended.Sub(start) >= cfg.Duration {
			break
		}
		// Ждём своего момента. Опоздали (генератор не успевал) — не ждём,
		// но intended остаётся плановым: опоздание войдёт в задержку.
		if d := time.Until(intended); d > 0 {
			timer.Reset(d)
			select {
			case <-ctx.Done():
				wg.Wait()
				stats.Elapsed = time.Since(start)
				return
			case <-timer.C:
			}
		}
		select {
		case sem <- struct{}{}:
		default:
			stats.add(func(s *Stats) { s.Dropped++ })
			continue
		}
		sc := pick(mix, picker)
		r := rand.New(rand.NewPCG(cfg.Seed, uint64(i)+1))
		stats.add(func(s *Stats) { s.Started++ })
		wg.Go(func() {
			defer func() { <-sem }()
			sc.Run(ctx, c, r, intended)
		})
	}
	wg.Wait()
	stats.Elapsed = time.Since(start)
}

// ClosedConfig — закрытая модель.
type ClosedConfig struct {
	// Users — виртуальных пользователей (> 0).
	Users int
	// Think — пауза между сценариями пользователя; фактическая —
	// равномерно в [0.5·Think, 1.5·Think], чтобы пользователи не ходили
	// строем.
	Think    time.Duration
	Duration time.Duration
	Seed     uint64
}

// RunClosed выполняет закрытую модель и заполняет stats.
func RunClosed(ctx context.Context, c *Client, mix []Scenario, cfg ClosedConfig, stats *Stats) {
	ctx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()
	start := time.Now()
	var wg sync.WaitGroup
	for u := range cfg.Users {
		r := rand.New(rand.NewPCG(cfg.Seed, uint64(u)+1))
		wg.Go(func() {
			timer := time.NewTimer(0)
			defer timer.Stop()
			<-timer.C
			for ctx.Err() == nil {
				stats.add(func(s *Stats) { s.Started++ })
				// Запросы, оборванные концом прогона, в статистику не
				// попадают как ошибки: исход "canceled" виден отдельно.
				pick(mix, r).Run(ctx, c, r, time.Now())
				if cfg.Think <= 0 {
					continue
				}
				think := time.Duration((0.5 + r.Float64()) * float64(cfg.Think))
				timer.Reset(think)
				select {
				case <-ctx.Done():
				case <-timer.C:
				}
			}
		})
	}
	wg.Wait()
	stats.Elapsed = time.Since(start)
}

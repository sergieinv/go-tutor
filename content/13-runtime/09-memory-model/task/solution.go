package task

import (
	"sync"
	"sync/atomic"
)

// Config — конфигурация сервиса. После публикации её не меняют:
// новая конфигурация — новый объект.
type Config struct {
	Name   string
	Limits map[string]int
}

// Service хранит текущую конфигурацию, флаг готовности и счётчик
// обращений. Все методы безопасны для одновременного вызова.
type Service struct {
	load      func() *Config
	loadOnce  sync.Once
	cfg       atomic.Pointer[Config]
	readyOnce sync.Once
	done      chan struct{}
	hits      atomic.Int64
}

// New создаёт сервис. load вызывается лениво — при первом Config.
func New(load func() *Config) *Service {
	return &Service{load: load, done: make(chan struct{})}
}

// Config возвращает текущую конфигурацию.
func (s *Service) Config() *Config {
	// Быстрый путь — атомарное чтение: если Load увидел указатель,
	// он видит и все поля, записанные до Store.
	if c := s.cfg.Load(); c != nil {
		return c
	}
	// Медленный путь — один раз на всё время жизни сервиса.
	// CompareAndSwap не затирает конфигурацию, которую успел
	// опубликовать Reload.
	s.loadOnce.Do(func() {
		s.cfg.CompareAndSwap(nil, s.load())
	})
	return s.cfg.Load()
}

// Reload публикует новую конфигурацию.
func (s *Service) Reload(c *Config) {
	s.cfg.Store(c)
}

// MarkReady отмечает сервис готовым и закрывает канал Done.
// Повторные и одновременные вызовы безопасны.
func (s *Service) MarkReady() {
	s.readyOnce.Do(func() { close(s.done) })
}

// Ready сообщает, вызывался ли MarkReady. Закрытый канал и есть
// флаг: закрытие synchronized before получения нулевого значения.
func (s *Service) Ready() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Done возвращает канал, который закрывается при MarkReady.
func (s *Service) Done() <-chan struct{} {
	return s.done
}

// Hit учитывает одно обращение.
func (s *Service) Hit() {
	s.hits.Add(1)
}

// Hits возвращает число обращений.
func (s *Service) Hits() int64 {
	return s.hits.Load()
}

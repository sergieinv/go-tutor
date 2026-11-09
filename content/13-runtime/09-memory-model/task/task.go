package task

import "sync"

// Config — конфигурация сервиса. После публикации её не меняют:
// новая конфигурация — новый объект.
type Config struct {
	Name   string
	Limits map[string]int
}

// Service хранит текущую конфигурацию, флаг готовности и счётчик
// обращений. В одной горутине всё работает, но методы вызывают
// из многих горутин одновременно — и тогда здесь гонки данных.
type Service struct {
	mu    sync.Mutex
	load  func() *Config
	cfg   *Config
	ready bool
	done  chan struct{}
	hits  int64
}

// New создаёт сервис. load вызывается лениво — при первом Config.
func New(load func() *Config) *Service {
	return &Service{load: load, done: make(chan struct{})}
}

// Config возвращает текущую конфигурацию.
func (s *Service) Config() *Config {
	if s.cfg == nil { // быстрая проверка без блокировки
		s.mu.Lock()
		if s.cfg == nil {
			s.cfg = s.load()
		}
		s.mu.Unlock()
	}
	return s.cfg
}

// Reload публикует новую конфигурацию.
func (s *Service) Reload(c *Config) {
	s.mu.Lock()
	s.cfg = c
	s.mu.Unlock()
}

// MarkReady отмечает сервис готовым и закрывает канал Done.
func (s *Service) MarkReady() {
	if !s.ready {
		s.ready = true
		close(s.done)
	}
}

// Ready сообщает, вызывался ли MarkReady.
func (s *Service) Ready() bool {
	return s.ready
}

// Done возвращает канал, который закрывается при MarkReady.
func (s *Service) Done() <-chan struct{} {
	return s.done
}

// Hit учитывает одно обращение.
func (s *Service) Hit() {
	s.hits++
}

// Hits возвращает число обращений.
func (s *Service) Hits() int64 {
	return s.hits
}

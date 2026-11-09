package task

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrNoBackend        = errors.New("no backend available")
	ErrUnknownPlayer    = errors.New("unknown player")
	ErrAlreadyConnected = errors.New("player already connected")
)

type Backend struct {
	Name  string
	World string
	Max   int
}

type Mover interface {
	Save(backend, player string) ([]byte, error)
	Load(backend, player string, state []byte) error
}

type backendState struct {
	Backend
	healthy bool
	load    int // игроки + резервы переносов
}

type Router struct {
	mover    Mover
	mu       sync.Mutex
	backends []*backendState
	byName   map[string]*backendState
	where    map[string]*backendState
}

func NewRouter(backends []Backend, m Mover) *Router {
	r := &Router{mover: m, byName: map[string]*backendState{}, where: map[string]*backendState{}}
	for _, b := range backends {
		s := &backendState{Backend: b, healthy: true}
		r.backends = append(r.backends, s)
		r.byName[b.Name] = s
	}
	return r
}

// pick выбирает и резервирует место; вызывать под r.mu.
func (r *Router) pick(world string) (*backendState, error) {
	var best *backendState
	for _, b := range r.backends {
		if b.World != world || !b.healthy || b.load >= b.Max {
			continue
		}
		if best == nil || b.load < best.load {
			best = b
		}
	}
	if best == nil {
		return nil, fmt.Errorf("world %q: %w", world, ErrNoBackend)
	}
	best.load++
	return best, nil
}

func (r *Router) SetHealthy(name string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b := r.byName[name]; b != nil {
		b.healthy = ok
	}
}

func (r *Router) Connect(player, world string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.where[player]; ok {
		return "", ErrAlreadyConnected
	}
	b, err := r.pick(world)
	if err != nil {
		return "", err
	}
	r.where[player] = b
	return b.Name, nil
}

func (r *Router) Transfer(player, world string) (string, error) {
	r.mu.Lock()
	from, ok := r.where[player]
	if !ok {
		r.mu.Unlock()
		return "", ErrUnknownPlayer
	}
	if from.World == world {
		r.mu.Unlock()
		return from.Name, nil
	}
	to, err := r.pick(world) // место на цели зарезервировано
	r.mu.Unlock()
	if err != nil {
		return "", err
	}
	cancel := func() {
		r.mu.Lock()
		to.load--
		r.mu.Unlock()
	}
	// Сеть — без блокировки: другие игроки не ждут нашего переноса.
	state, err := r.mover.Save(from.Name, player)
	if err != nil {
		cancel()
		return "", fmt.Errorf("save %s on %s: %w", player, from.Name, err)
	}
	if err := r.mover.Load(to.Name, player, state); err != nil {
		cancel()
		return "", fmt.Errorf("load %s on %s: %w", player, to.Name, err)
	}
	r.mu.Lock()
	from.load--
	r.where[player] = to
	r.mu.Unlock()
	return to.Name, nil
}

func (r *Router) Disconnect(player string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.where[player]
	if !ok {
		return ErrUnknownPlayer
	}
	b.load--
	delete(r.where, player)
	return nil
}

func (r *Router) Where(player string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.where[player]; ok {
		return b.Name, true
	}
	return "", false
}

func (r *Router) Load(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b := r.byName[name]; b != nil {
		return b.load
	}
	return 0
}

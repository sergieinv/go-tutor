package task

import "errors"

var (
	ErrNoBackend        = errors.New("no backend available")
	ErrUnknownPlayer    = errors.New("unknown player")
	ErrAlreadyConnected = errors.New("player already connected")
)

// Backend — сервер за прокси: имя, мир и вместимость.
type Backend struct {
	Name  string
	World string
	Max   int
}

// Mover переносит состояние игрока между бэкендами (по сети).
type Mover interface {
	Save(backend, player string) ([]byte, error)
	Load(backend, player string, state []byte) error
}

type Router struct {
	// TODO
}

func NewRouter(backends []Backend, m Mover) *Router {
	// TODO
	return &Router{}
}

func (r *Router) SetHealthy(name string, ok bool) {
	// TODO
}

func (r *Router) Connect(player, world string) (string, error) {
	// TODO
	return "", nil
}

func (r *Router) Transfer(player, world string) (string, error) {
	// TODO
	return "", nil
}

func (r *Router) Disconnect(player string) error {
	// TODO
	return nil
}

func (r *Router) Where(player string) (string, bool) {
	// TODO
	return "", false
}

func (r *Router) Load(name string) int {
	// TODO
	return 0
}

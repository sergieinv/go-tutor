package task

import (
	"errors"
	"time"
)

const ProtocolVersion = 767

type State int

const (
	StateHandshake State = iota
	StateStatus
	StateLogin
	StatePlay
	StateClosed
)

var (
	ErrUnexpectedPacket    = errors.New("unexpected packet for state")
	ErrBadNextState        = errors.New("bad next state")
	ErrUnsupportedProtocol = errors.New("unsupported protocol version")
	ErrBadName             = errors.New("bad player name")
	ErrBadChat             = errors.New("bad chat message")
	ErrBadKeepAlive        = errors.New("bad keep-alive response")
	ErrKeepAliveTimeout    = errors.New("keep-alive timeout")
	ErrClosed              = errors.New("connection closed")
)

// Входящие пакеты.
type Handshake struct {
	Protocol  int32
	NextState int32
}
type StatusRequest struct{}
type PingRequest struct{ Payload int64 }
type LoginStart struct{ Name string }
type KeepAliveResponse struct{ ID int64 }
type ChatMessage struct{ Text string }

// Исходящие пакеты.
type StatusResponse struct{ JSON string }
type PongResponse struct{ Payload int64 }
type LoginSuccess struct {
	UUID [16]byte
	Name string
}
type KeepAlive struct{ ID int64 }
type ChatBroadcast struct{ Name, Text string }

type Config struct {
	VersionName       string
	Description       string
	MaxPlayers        int
	Online            int
	KeepAliveInterval time.Duration
	KeepAliveTimeout  time.Duration
}

type Session struct {
	// TODO: состояние сессии
}

func NewSession(cfg Config) *Session { return &Session{} }

func (s *Session) State() State { return StateHandshake }

// OfflineUUID — UUID версии 3 от "OfflinePlayer:"+name.
func OfflineUUID(name string) [16]byte {
	// TODO
	return [16]byte{}
}

// Handle обрабатывает входящий пакет p, пришедший в момент now.
func (s *Session) Handle(now time.Time, p any) ([]any, error) {
	// TODO
	return nil, nil
}

// Tick вызывается сервером периодически: отправка keep-alive и таймаут.
func (s *Session) Tick(now time.Time) ([]any, error) {
	// TODO
	return nil, nil
}

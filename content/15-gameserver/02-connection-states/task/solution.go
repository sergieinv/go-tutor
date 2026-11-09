package task

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"time"
	"unicode"
	"unicode/utf8"
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
	cfg       Config
	state     State
	statusSet bool // StatusRequest уже был
	name      string
	lastKA    time.Time // когда отправлен последний keep-alive (или вход)
	nextID    int64
	pending   int64 // id keep-alive, на который ждём ответ; 0 — не ждём
}

func NewSession(cfg Config) *Session { return &Session{cfg: cfg} }

func (s *Session) State() State { return s.state }

func OfflineUUID(name string) [16]byte {
	u := md5.Sum([]byte("OfflinePlayer:" + name))
	u[6] = u[6]&0x0f | 0x30
	u[8] = u[8]&0x3f | 0x80
	return u
}

func (s *Session) Handle(now time.Time, p any) ([]any, error) {
	if s.state == StateClosed {
		return nil, ErrClosed
	}
	out, err := s.handle(now, p)
	if err != nil {
		s.state = StateClosed
		return nil, err
	}
	return out, nil
}

func (s *Session) handle(now time.Time, p any) ([]any, error) {
	if s.state == StatePlay && s.pending != 0 && now.Sub(s.lastKA) >= s.cfg.KeepAliveTimeout {
		return nil, ErrKeepAliveTimeout
	}
	switch s.state {
	case StateHandshake:
		if h, ok := p.(Handshake); ok {
			switch h.NextState {
			case 1:
				s.state = StateStatus
			case 2:
				if h.Protocol != ProtocolVersion {
					return nil, ErrUnsupportedProtocol
				}
				s.state = StateLogin
			default:
				return nil, ErrBadNextState
			}
			return nil, nil
		}
	case StateStatus:
		switch p := p.(type) {
		case StatusRequest:
			if s.statusSet {
				return nil, ErrUnexpectedPacket
			}
			s.statusSet = true
			return []any{StatusResponse{s.statusJSON()}}, nil
		case PingRequest:
			s.state = StateClosed
			return []any{PongResponse{p.Payload}}, nil
		}
	case StateLogin:
		if l, ok := p.(LoginStart); ok {
			if !validName(l.Name) {
				return nil, ErrBadName
			}
			s.name = l.Name
			s.state = StatePlay
			s.lastKA = now
			return []any{LoginSuccess{OfflineUUID(l.Name), l.Name}}, nil
		}
	case StatePlay:
		switch p := p.(type) {
		case KeepAliveResponse:
			if s.pending == 0 || p.ID != s.pending {
				return nil, ErrBadKeepAlive
			}
			s.pending = 0
			return nil, nil
		case ChatMessage:
			if !validChat(p.Text) {
				return nil, ErrBadChat
			}
			return []any{ChatBroadcast{s.name, p.Text}}, nil
		}
	}
	return nil, ErrUnexpectedPacket
}

func (s *Session) Tick(now time.Time) ([]any, error) {
	switch s.state {
	case StateClosed:
		return nil, ErrClosed
	case StatePlay:
	default:
		return nil, nil
	}
	if s.pending != 0 {
		if now.Sub(s.lastKA) >= s.cfg.KeepAliveTimeout {
			s.state = StateClosed
			return nil, ErrKeepAliveTimeout
		}
		return nil, nil
	}
	if now.Sub(s.lastKA) >= s.cfg.KeepAliveInterval {
		s.nextID++
		s.pending = s.nextID
		s.lastKA = now
		return []any{KeepAlive{s.pending}}, nil
	}
	return nil, nil
}

func (s *Session) statusJSON() string {
	var st struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
		} `json:"players"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	st.Version.Name = s.cfg.VersionName
	st.Version.Protocol = ProtocolVersion
	st.Players.Max = s.cfg.MaxPlayers
	st.Players.Online = s.cfg.Online
	st.Description.Text = s.cfg.Description
	b, _ := json.Marshal(st)
	return string(b)
}

func validName(name string) bool {
	if len(name) < 3 || len(name) > 16 {
		return false
	}
	for _, c := range []byte(name) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func validChat(text string) bool {
	if len(text) == 0 || len(text) > 256 || !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

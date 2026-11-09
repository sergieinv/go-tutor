package task

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func cfg() Config {
	return Config{
		VersionName:       "1.21.1",
		Description:       "Учебный сервер",
		MaxPlayers:        20,
		Online:            3,
		KeepAliveInterval: 15 * time.Second,
		KeepAliveTimeout:  30 * time.Second,
	}
}

func TestStatusFlow(t *testing.T) {
	s := NewSession(cfg())
	if out, err := s.Handle(t0, Handshake{Protocol: 5, NextState: 1}); err != nil || len(out) != 0 {
		t.Fatalf("Handshake: %v, %v", out, err)
	}
	if s.State() != StateStatus {
		t.Fatalf("состояние %v, want StateStatus", s.State())
	}
	out, err := s.Handle(t0, StatusRequest{})
	if err != nil || len(out) != 1 {
		t.Fatalf("StatusRequest: %v, %v", out, err)
	}
	resp, ok := out[0].(StatusResponse)
	if !ok {
		t.Fatalf("ответ %T, want StatusResponse", out[0])
	}
	var st struct {
		Version struct {
			Name     string
			Protocol int
		}
		Players     struct{ Max, Online int }
		Description struct{ Text string }
	}
	if err := json.Unmarshal([]byte(resp.JSON), &st); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if st.Version.Name != "1.21.1" || st.Version.Protocol != 767 || st.Players.Max != 20 || st.Players.Online != 3 || st.Description.Text != "Учебный сервер" {
		t.Fatalf("status = %+v", st)
	}
	out, err = s.Handle(t0, PingRequest{Payload: 42})
	if err != nil || len(out) != 1 || out[0] != (PongResponse{42}) {
		t.Fatalf("Ping: %v, %v", out, err)
	}
	if s.State() != StateClosed {
		t.Fatalf("после Pong состояние %v, want StateClosed", s.State())
	}
}

func TestOfflineUUID(t *testing.T) {
	u := OfflineUUID("Steve")
	if got := hex.EncodeToString(u[:]); got != "5627dd98e6be3c21b8a8e92344183641" {
		t.Fatalf("OfflineUUID(Steve) = %s", got)
	}
}

func TestLoginAndKeepAlive(t *testing.T) {
	s := NewSession(cfg())
	s.Handle(t0, Handshake{Protocol: ProtocolVersion, NextState: 2})
	out, err := s.Handle(t0, LoginStart{Name: "Steve"})
	if err != nil || len(out) != 1 || out[0] != (LoginSuccess{OfflineUUID("Steve"), "Steve"}) {
		t.Fatalf("LoginStart: %v, %v", out, err)
	}
	if s.State() != StatePlay {
		t.Fatalf("состояние %v, want StatePlay", s.State())
	}
	if out, _ := s.Tick(t0.Add(14 * time.Second)); len(out) != 0 {
		t.Fatalf("keep-alive раньше интервала: %v", out)
	}
	out, err = s.Tick(t0.Add(15 * time.Second))
	if err != nil || len(out) != 1 || out[0] != (KeepAlive{1}) {
		t.Fatalf("Tick через 15 с: %v, %v", out, err)
	}
	if out, err := s.Handle(t0.Add(16*time.Second), KeepAliveResponse{1}); err != nil || len(out) != 0 {
		t.Fatalf("ответ на keep-alive: %v, %v", out, err)
	}
	out, err = s.Handle(t0.Add(17*time.Second), ChatMessage{"привет"})
	if err != nil || len(out) != 1 || out[0] != (ChatBroadcast{"Steve", "привет"}) {
		t.Fatalf("чат: %v, %v", out, err)
	}
	out, err = s.Tick(t0.Add(30 * time.Second))
	if err != nil || len(out) != 1 || out[0] != (KeepAlive{2}) {
		t.Fatalf("второй keep-alive: %v, %v", out, err)
	}
	if _, err := s.Tick(t0.Add(60 * time.Second)); !errors.Is(err, ErrKeepAliveTimeout) {
		t.Fatalf("молчание 30 с: err = %v, want ErrKeepAliveTimeout", err)
	}
	if s.State() != StateClosed {
		t.Fatalf("состояние %v, want StateClosed", s.State())
	}
}

func TestUnexpectedPacket(t *testing.T) {
	s := NewSession(cfg())
	if _, err := s.Handle(t0, ChatMessage{"hi"}); !errors.Is(err, ErrUnexpectedPacket) {
		t.Fatalf("чат в handshake: err = %v, want ErrUnexpectedPacket", err)
	}
	if s.State() != StateClosed {
		t.Fatalf("состояние %v, want StateClosed", s.State())
	}
	if _, err := s.Handle(t0, Handshake{ProtocolVersion, 1}); !errors.Is(err, ErrClosed) {
		t.Fatalf("после закрытия: err = %v, want ErrClosed", err)
	}
}

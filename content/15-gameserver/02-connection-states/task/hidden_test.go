package task

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func toPlay(t *testing.T, c Config, name string) *Session {
	t.Helper()
	s := NewSession(c)
	if _, err := s.Handle(t0, Handshake{ProtocolVersion, 2}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if _, err := s.Handle(t0, LoginStart{name}); err != nil {
		t.Fatalf("LoginStart(%q): %v", name, err)
	}
	return s
}

func expectClosed(t *testing.T, s *Session, what string) {
	t.Helper()
	if s.State() != StateClosed {
		t.Errorf("%s: состояние %v, want StateClosed", what, s.State())
	}
	if out, err := s.Handle(t0.Add(time.Hour), StatusRequest{}); !errors.Is(err, ErrClosed) || out != nil {
		t.Errorf("%s: Handle после закрытия: %v, %v; want nil, ErrClosed", what, out, err)
	}
	if out, err := s.Tick(t0.Add(time.Hour)); !errors.Is(err, ErrClosed) || out != nil {
		t.Errorf("%s: Tick после закрытия: %v, %v; want nil, ErrClosed", what, out, err)
	}
}

// Таблица «состояние × пакет»: всё, чего нет в таблице, — ErrUnexpectedPacket.
func TestHiddenTransitionTable(t *testing.T) {
	all := []any{Handshake{ProtocolVersion, 1}, StatusRequest{}, PingRequest{1}, LoginStart{"Steve"}, KeepAliveResponse{1}, ChatMessage{"hi"}, nil, "мусор", &StatusRequest{}}
	allowed := map[State]map[int]bool{
		StateHandshake: {0: true},
		StateStatus:    {1: true, 2: true},
		StateLogin:     {3: true},
		StatePlay:      {5: true}, // KeepAliveResponse без ожидания — отдельная ошибка
	}
	reach := map[State]func() *Session{
		StateHandshake: func() *Session { return NewSession(cfg()) },
		StateStatus: func() *Session {
			s := NewSession(cfg())
			s.Handle(t0, Handshake{1, 1})
			return s
		},
		StateLogin: func() *Session {
			s := NewSession(cfg())
			s.Handle(t0, Handshake{ProtocolVersion, 2})
			return s
		},
		StatePlay: func() *Session { return toPlay(t, cfg(), "Steve") },
	}
	for st, mk := range reach {
		for i, p := range all {
			s := mk()
			if s.State() != st {
				t.Fatalf("не удалось попасть в состояние %v", st)
			}
			out, err := s.Handle(t0, p)
			if allowed[st][i] {
				if err != nil {
					t.Errorf("%v + %#v: неожиданная ошибка %v", st, p, err)
				}
				continue
			}
			if st == StatePlay && i == 4 {
				if !errors.Is(err, ErrBadKeepAlive) {
					t.Errorf("play + KeepAliveResponse без keep-alive: err = %v, want ErrBadKeepAlive", err)
				}
			} else if !errors.Is(err, ErrUnexpectedPacket) {
				t.Errorf("%v + %#v: err = %v, want ErrUnexpectedPacket", st, p, err)
			}
			if out != nil {
				t.Errorf("%v + %#v: при ошибке пакеты должны быть nil, а не %v", st, p, out)
			}
			expectClosed(t, s, "после ошибки")
		}
	}
}

func TestHiddenHandshake(t *testing.T) {
	s := NewSession(cfg())
	if _, err := s.Handle(t0, Handshake{ProtocolVersion - 1, 2}); !errors.Is(err, ErrUnsupportedProtocol) {
		t.Errorf("старый протокол при входе: err = %v, want ErrUnsupportedProtocol", err)
	}
	expectClosed(t, s, "старый протокол")
	for _, next := range []int32{0, 3, -1, 100} {
		s := NewSession(cfg())
		if _, err := s.Handle(t0, Handshake{ProtocolVersion, next}); !errors.Is(err, ErrBadNextState) {
			t.Errorf("next state %d: err = %v, want ErrBadNextState", next, err)
		}
		expectClosed(t, s, "плохой next state")
	}
	// Для статуса версия протокола не важна: клиент другой версии
	// тоже видит сервер в списке.
	s = NewSession(cfg())
	if out, err := s.Handle(t0, Handshake{-7, 1}); err != nil || out != nil || s.State() != StateStatus {
		t.Errorf("статус с чужим протоколом: %v, %v, состояние %v", out, err, s.State())
	}
	if out, err := s.Handle(t0, StatusRequest{}); err != nil || len(out) != 1 {
		t.Errorf("StatusRequest: %v, %v", out, err)
	}
}

func TestHiddenStatus(t *testing.T) {
	c := Config{VersionName: `go "tutor"`, Description: "строка\nс переводом", MaxPlayers: 0, Online: 0}
	s := NewSession(c)
	s.Handle(t0, Handshake{1, 1})
	out, err := s.Handle(t0, StatusRequest{})
	if err != nil || len(out) != 1 {
		t.Fatalf("StatusRequest: %v, %v", out, err)
	}
	var m map[string]map[string]any
	if err := json.Unmarshal([]byte(out[0].(StatusResponse).JSON), &m); err != nil {
		t.Fatalf("JSON (экранирование?): %v", err)
	}
	if m["version"]["name"] != `go "tutor"` || m["version"]["protocol"] != float64(767) {
		t.Errorf("version = %v", m["version"])
	}
	if p, ok := m["players"]["max"]; !ok || p != float64(0) {
		t.Errorf("players.max = %v, %v (нулевое значение тоже должно быть в JSON)", p, ok)
	}
	if p, ok := m["players"]["online"]; !ok || p != float64(0) {
		t.Errorf("players.online = %v, %v", p, ok)
	}
	if m["description"]["text"] != "строка\nс переводом" {
		t.Errorf("description = %v", m["description"])
	}
	// Второй StatusRequest — ошибка.
	if _, err := s.Handle(t0, StatusRequest{}); !errors.Is(err, ErrUnexpectedPacket) {
		t.Errorf("второй StatusRequest: err = %v, want ErrUnexpectedPacket", err)
	}
	// Ping без StatusRequest допустим; payload — любой, в том числе отрицательный.
	s = NewSession(c)
	s.Handle(t0, Handshake{1, 1})
	if out, err := s.Handle(t0, PingRequest{-1 << 63}); err != nil || len(out) != 1 || out[0] != (PongResponse{-1 << 63}) {
		t.Errorf("Ping без статуса: %v, %v", out, err)
	}
	expectClosed(t, s, "после Pong")
	// Tick в status ничего не делает.
	s = NewSession(c)
	s.Handle(t0, Handshake{1, 1})
	if out, err := s.Tick(t0.Add(time.Hour)); out != nil || err != nil || s.State() != StateStatus {
		t.Errorf("Tick в status: %v, %v, %v", out, err, s.State())
	}
}

func TestHiddenNames(t *testing.T) {
	good := []string{"abc", "Steve", "x_Y_9", strings.Repeat("a", 16), "___"}
	bad := []string{"", "ab", strings.Repeat("a", 17), "Сергей", "a b c", "abc!", "abc\x00", "ab-cd", "notch\n"}
	for _, n := range good {
		s := NewSession(cfg())
		s.Handle(t0, Handshake{ProtocolVersion, 2})
		out, err := s.Handle(t0, LoginStart{n})
		if err != nil || len(out) != 1 || out[0] != (LoginSuccess{OfflineUUID(n), n}) || s.State() != StatePlay {
			t.Errorf("имя %q: %v, %v", n, out, err)
		}
	}
	for _, n := range bad {
		s := NewSession(cfg())
		s.Handle(t0, Handshake{ProtocolVersion, 2})
		if out, err := s.Handle(t0, LoginStart{n}); !errors.Is(err, ErrBadName) || out != nil {
			t.Errorf("имя %q: %v, %v; want nil, ErrBadName", n, out, err)
		}
		expectClosed(t, s, "плохое имя")
	}
}

func TestHiddenUUIDBits(t *testing.T) {
	for _, n := range []string{"Steve", "Alex", "Notch", "a"} {
		u := OfflineUUID(n)
		if u[6]>>4 != 3 {
			t.Errorf("%s: версия %d, want 3", n, u[6]>>4)
		}
		if u[8]>>6 != 2 {
			t.Errorf("%s: вариант %b, want 10", n, u[8]>>6)
		}
	}
	// Остальные 122 бита — MD5 от "OfflinePlayer:"+имя без изменений.
	sum := md5.Sum([]byte("OfflinePlayer:Notch"))
	u := OfflineUUID("Notch")
	for i := range u {
		if i == 6 || i == 8 {
			continue
		}
		if u[i] != sum[i] {
			t.Fatalf("байт %d: %02x, want %02x (MD5 от \"OfflinePlayer:Notch\")", i, u[i], sum[i])
		}
	}
	if u[6]&0x0f != sum[6]&0x0f || u[8]&0x3f != sum[8]&0x3f {
		t.Errorf("младшие биты байтов 6 и 8 должны остаться от MD5")
	}
	if OfflineUUID("Alex") == OfflineUUID("alex") {
		t.Errorf("имена с разным регистром должны давать разные UUID")
	}
}

func TestHiddenChat(t *testing.T) {
	good := []string{"a", strings.Repeat("я", 128), "эмодзи 😀", "§ — не управляющий"}
	bad := []string{"", strings.Repeat("a", 257), strings.Repeat("я", 129), "a\nb", "tab\t", "\x7f", "\x00", "\u0085", "\xff\xfe"}
	for _, m := range good {
		s := toPlay(t, cfg(), "Steve")
		out, err := s.Handle(t0, ChatMessage{m})
		if err != nil || len(out) != 1 || out[0] != (ChatBroadcast{"Steve", m}) {
			t.Errorf("чат %q: %v, %v", m, out, err)
		}
	}
	for _, m := range bad {
		s := toPlay(t, cfg(), "Steve")
		if out, err := s.Handle(t0, ChatMessage{m}); !errors.Is(err, ErrBadChat) || out != nil {
			t.Errorf("чат %q: %v, %v; want nil, ErrBadChat", m, out, err)
		}
		expectClosed(t, s, "плохой чат")
	}
}

func TestHiddenKeepAliveSchedule(t *testing.T) {
	c := cfg()
	c.KeepAliveInterval = 10 * time.Second
	c.KeepAliveTimeout = 5 * time.Second
	login := t0.Add(100 * time.Second) // время входа — отсчёт первого интервала
	s := NewSession(c)
	s.Handle(t0, Handshake{ProtocolVersion, 2})
	s.Handle(login, LoginStart{"Steve"})
	at := func(d time.Duration) time.Time { return login.Add(d) }

	if out, err := s.Tick(at(9 * time.Second)); out != nil || err != nil {
		t.Fatalf("9 с: %v, %v", out, err)
	}
	for i := int64(1); i <= 3; i++ {
		base := time.Duration(i) * 10 * time.Second
		out, err := s.Tick(at(base))
		if err != nil || len(out) != 1 || out[0] != (KeepAlive{i}) {
			t.Fatalf("keep-alive %d на %v: %v, %v", i, base, out, err)
		}
		// Пока ждём ответа, новых keep-alive нет.
		if out, err := s.Tick(at(base + 4*time.Second)); out != nil || err != nil {
			t.Fatalf("пока ждём ответ %d: %v, %v", i, out, err)
		}
		// Ответ после интервала, но до таймаута.
		if out, err := s.Handle(at(base+4*time.Second), KeepAliveResponse{i}); out != nil || err != nil {
			t.Fatalf("ответ %d: %v, %v", i, out, err)
		}
	}
	// Следующий отсчитывается от момента отправки предыдущего, а не от ответа.
	if out, _ := s.Tick(at(40 * time.Second)); len(out) != 1 || out[0] != (KeepAlive{4}) {
		t.Fatalf("keep-alive 4: %v", out)
	}
	// Ответ, пришедший ровно на таймауте, уже опоздал.
	if out, err := s.Handle(at(45*time.Second), KeepAliveResponse{4}); !errors.Is(err, ErrKeepAliveTimeout) || out != nil {
		t.Fatalf("ответ на таймауте: %v, %v; want nil, ErrKeepAliveTimeout", out, err)
	}
	expectClosed(t, s, "таймаут keep-alive")
}

func TestHiddenKeepAliveWrong(t *testing.T) {
	c := cfg()
	// Чужой id.
	s := toPlay(t, c, "Steve")
	s.Tick(t0.Add(15 * time.Second))
	if _, err := s.Handle(t0.Add(16*time.Second), KeepAliveResponse{2}); !errors.Is(err, ErrBadKeepAlive) {
		t.Errorf("чужой id: err = %v, want ErrBadKeepAlive", err)
	}
	expectClosed(t, s, "чужой id")
	// Повторный ответ на уже подтверждённый keep-alive.
	s = toPlay(t, c, "Steve")
	s.Tick(t0.Add(15 * time.Second))
	s.Handle(t0.Add(16*time.Second), KeepAliveResponse{1})
	if _, err := s.Handle(t0.Add(17*time.Second), KeepAliveResponse{1}); !errors.Is(err, ErrBadKeepAlive) {
		t.Errorf("повторный ответ: err = %v, want ErrBadKeepAlive", err)
	}
	// Чат после таймаута тоже закрывает соединение по таймауту.
	s = toPlay(t, c, "Steve")
	s.Tick(t0.Add(15 * time.Second))
	if _, err := s.Handle(t0.Add(45*time.Second), ChatMessage{"hi"}); !errors.Is(err, ErrKeepAliveTimeout) {
		t.Errorf("чат после таймаута: err = %v, want ErrKeepAliveTimeout", err)
	}
	// Чат до таймаута работает, пока ждём ответа.
	s = toPlay(t, c, "Steve")
	s.Tick(t0.Add(15 * time.Second))
	if out, err := s.Handle(t0.Add(44*time.Second), ChatMessage{"hi"}); err != nil || len(out) != 1 {
		t.Errorf("чат до таймаута: %v, %v", out, err)
	}
	// Tick до входа ничего не отправляет.
	s = NewSession(c)
	if out, err := s.Tick(t0.Add(time.Hour)); out != nil || err != nil {
		t.Errorf("Tick в handshake: %v, %v", out, err)
	}
	s.Handle(t0, Handshake{ProtocolVersion, 2})
	if out, err := s.Tick(t0.Add(time.Hour)); out != nil || err != nil {
		t.Errorf("Tick в login: %v, %v", out, err)
	}
}

// Сессии независимы: счётчик id keep-alive у каждой свой.
func TestHiddenIndependentSessions(t *testing.T) {
	a := toPlay(t, cfg(), "Alice")
	b := toPlay(t, cfg(), "Bob_1")
	a.Tick(t0.Add(15 * time.Second))
	a.Handle(t0.Add(15*time.Second), KeepAliveResponse{1})
	if out, _ := b.Tick(t0.Add(15 * time.Second)); len(out) != 1 || out[0] != (KeepAlive{1}) {
		t.Errorf("первый keep-alive второй сессии: %v, want KeepAlive{1}", out)
	}
}

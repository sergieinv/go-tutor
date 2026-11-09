package task

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestHiddenGeneratedRequestID(t *testing.T) {
	e := newEnv(t, tokens, whoami)
	for i, want := range []string{"alice/gen-1", "alice/gen-2"} {
		kv := []string{"authorization", "Bearer a-token"}
		if i == 1 {
			kv = append(kv, "x-request-id", "") // пустой — как отсутствующий
		}
		got, header, err := e.say(t, "hi", kv...)
		if err != nil {
			t.Fatalf("ошибка: %v", err)
		}
		if got != want {
			t.Errorf("обработчик увидел %q, want %q", got, want)
		}
		if h := header.Get("x-request-id"); !slices.Equal(h, []string{want[len("alice/"):]}) {
			t.Errorf("заголовок ответа x-request-id = %q, want %q", h, want[len("alice/"):])
		}
	}
	want := []string{"/test.Echo/Say OK rid=gen-1", "/test.Echo/Say OK rid=gen-2"}
	if logs := e.logs(); !slices.Equal(logs, want) {
		t.Errorf("лог %q, want %q", logs, want)
	}
}

func TestHiddenClientRequestIDEchoed(t *testing.T) {
	e := newEnv(t, tokens, whoami)
	_, header, err := e.say(t, "hi", "x-request-id", "abc", "x-request-id", "def")
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("код %v, want Unauthenticated", status.Code(err))
	}
	// Заголовок отправлен даже при отказе: request id нужен и для разбора ошибок.
	if h := header.Get("x-request-id"); !slices.Equal(h, []string{"abc"}) {
		t.Errorf("заголовок ответа x-request-id = %q, want [abc] (первое значение)", h)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ids != 0 {
		t.Errorf("NewID вызван %d раз, хотя клиент прислал request id", e.ids)
	}
}

func TestHiddenBadAuthorization(t *testing.T) {
	cases := map[string][]string{
		"чужой токен":      {"authorization", "Bearer nope"},
		"схема строчными":  {"authorization", "bearer a-token"},
		"без схемы":        {"authorization", "a-token"},
		"пустой токен":     {"authorization", "Bearer "},
		"два значения":     {"authorization", "Bearer a-token", "authorization", "Bearer b-token"},
		"токен с пробелом": {"authorization", "Bearer  a-token"},
		"префикс токена":   {"authorization", "Bearer a-tok"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			var called atomic.Bool
			e := newEnv(t, map[string]string{"alice": "a-token", "bob": "b-token", "ghost": ""}, func(ctx context.Context, in string) (string, error) {
				called.Store(true)
				return "", nil
			})
			_, _, err := e.say(t, "hi", append(kv, "x-request-id", "r")...)
			st := status.Convert(err)
			if st.Code() != codes.Unauthenticated || st.Message() != "unauthenticated" {
				t.Fatalf("ошибка %v, want Unauthenticated «unauthenticated»", err)
			}
			if called.Load() {
				t.Error("обработчик вызван")
			}
			want := []string{"/test.Echo/Say Unauthenticated rid=r"}
			if logs := e.logs(); !slices.Equal(logs, want) {
				t.Errorf("лог %q, want %q", logs, want)
			}
		})
	}
}

func TestHiddenHealthWithoutToken(t *testing.T) {
	e := newEnv(t, tokens, whoami)
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-request-id", "hc")
	resp, err := healthpb.NewHealthClient(e.conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health без токена: %v", err)
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("статус %v", resp.GetStatus())
	}
	want := []string{"/grpc.health.v1.Health/Check OK rid=hc"}
	if logs := e.logs(); !slices.Equal(logs, want) {
		t.Errorf("лог %q, want %q", logs, want)
	}
}

func TestHiddenHandlerErrorPassesThrough(t *testing.T) {
	e := newEnv(t, tokens, func(ctx context.Context, in string) (string, error) {
		if in == "plain" {
			return "", errors.New("db is down")
		}
		return "", status.Error(codes.NotFound, "order 7 not found")
	})
	_, _, err := e.say(t, "x", "authorization", "Bearer a-token", "x-request-id", "r1")
	st := status.Convert(err)
	if st.Code() != codes.NotFound || st.Message() != "order 7 not found" {
		t.Errorf("ошибка %v, want NotFound «order 7 not found» без изменений", err)
	}
	_, _, err = e.say(t, "plain", "authorization", "Bearer a-token", "x-request-id", "r2")
	if status.Code(err) != codes.Unknown {
		t.Errorf("код обычной ошибки %v, want Unknown", status.Code(err))
	}
	want := []string{"/test.Echo/Say NotFound rid=r1", "/test.Echo/Say Unknown rid=r2"}
	if logs := e.logs(); !slices.Equal(logs, want) {
		t.Errorf("лог %q, want %q", logs, want)
	}
}

func TestHiddenPanicValueNotLeaked(t *testing.T) {
	e := newEnv(t, tokens, func(ctx context.Context, in string) (string, error) {
		panic(errors.New("pq: password authentication failed for user admin"))
	})
	_, _, err := e.say(t, "hi", "authorization", "Bearer b-token", "x-request-id", "p")
	st := status.Convert(err)
	if st.Code() != codes.Internal || st.Message() != "internal error" || len(st.Details()) != 0 {
		t.Fatalf("ошибка %v, want Internal «internal error» без деталей", err)
	}
	want := []string{"panic /test.Echo/Say rid=p: pq: password authentication failed for user admin", "/test.Echo/Say Internal rid=p"}
	if logs := e.logs(); !slices.Equal(logs, want) {
		t.Errorf("лог %q, want %q", logs, want)
	}
	// Сервер пережил панику и обслуживает следующий вызов.
	_, _, err = e.say(t, "hi", "x-request-id", "q")
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("после паники код %v, want Unauthenticated", status.Code(err))
	}
}

func TestHiddenContextWithoutValues(t *testing.T) {
	if got := RequestIDFrom(context.Background()); got != "" {
		t.Errorf("RequestIDFrom(пустой) = %q", got)
	}
	if got := UserFrom(context.Background()); got != "" {
		t.Errorf("UserFrom(пустой) = %q", got)
	}
}

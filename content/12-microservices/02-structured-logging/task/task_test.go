package task

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// newLogger — логгер с обёрткой над JSONHandler без времени.
func newLogger(level slog.Level) (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	jh := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	return slog.New(NewContextHandler(jh)), &buf
}

func lines(buf *bytes.Buffer) []string {
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func check(t *testing.T, buf *bytes.Buffer, want ...string) {
	t.Helper()
	got := lines(buf)
	if len(got) != len(want) {
		t.Fatalf("строк %d, want %d:\n%s", len(got), len(want), buf.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("строка %d:\n got  %s\n want %s", i+1, got[i], want[i])
		}
	}
}

// spanCtx — контекст с готовым SpanContext, как его положил бы OpenTelemetry.
func spanCtx(ctx context.Context) context.Context {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7},
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(ctx, sc)
}

func TestRequestAttrs(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := WithAttrs(context.Background(), slog.String("request_id", "r-1"))
	log.InfoContext(ctx, "create order", "qty", 2)
	log.Info("no context")
	check(t, buf,
		`{"level":"INFO","msg":"create order","request_id":"r-1","qty":2}`,
		`{"level":"INFO","msg":"no context"}`,
	)
}

func TestAttrsStayTopLevel(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := WithAttrs(context.Background(), slog.String("request_id", "r-1"))
	log.WithGroup("db").InfoContext(ctx, "query", "rows", 3)
	check(t, buf, `{"level":"INFO","msg":"query","request_id":"r-1","db":{"rows":3}}`)
}

func TestTraceIDs(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := spanCtx(WithAttrs(context.Background(), slog.String("request_id", "r-1")))
	log.InfoContext(ctx, "reserve")
	check(t, buf, `{"level":"INFO","msg":"reserve","request_id":"r-1","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7"}`)
}

package task

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// newLogger пишет JSON без времени в буфер.
func newLogger(level slog.Level) (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	next := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	})
	return slog.New(NewContextHandler(next)), &buf
}

func TestRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "r-1")
	if got := RequestID(ctx); got != "r-1" {
		t.Errorf("RequestID = %q, want %q", got, "r-1")
	}
	if got := RequestID(context.Background()); got != "" {
		t.Errorf("RequestID(Background) = %q, want \"\"", got)
	}
}

func TestHandlerAddsRequestID(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := WithRequestID(context.Background(), "r-42")
	log.InfoContext(ctx, "запрос", "path", "/api")
	want := `{"level":"INFO","msg":"запрос","path":"/api","request_id":"r-42"}`
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Errorf("вывод:\n%s\nwant:\n%s", got, want)
	}
}

func TestHandlerWithoutID(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	log.InfoContext(context.Background(), "фон")
	want := `{"level":"INFO","msg":"фон"}`
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Errorf("вывод:\n%s\nwant:\n%s", got, want)
	}
}

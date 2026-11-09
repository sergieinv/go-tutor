package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestHandlerAddsContextAttributes(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelInfo, "test")

	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := trace.SpanIDFromHex("0102030405060708")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(WithRequestID(context.Background(), "req-1"), sc)

	log.InfoContext(ctx, "hello", "k", "v")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, buf.String())
	}
	want := map[string]string{
		"msg":        "hello",
		"service":    "test",
		"k":          "v",
		"request_id": "req-1",
		"trace_id":   "0102030405060708090a0b0c0d0e0f10",
		"span_id":    "0102030405060708",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %q", k, got[k], v)
		}
	}
}

func TestHandlerWithoutContext(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, slog.LevelInfo, "test").Info("plain")
	if s := buf.String(); strings.Contains(s, "request_id") || strings.Contains(s, "trace_id") {
		t.Errorf("unexpected context attributes: %s", s)
	}
}

func TestValidRequestID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{NewRequestID(), true},
		{"abc-DEF_1.2", true},
		{"", false},
		{strings.Repeat("a", 65), false},
		{"a\nfake log line", false},
		{"a b", false},
		{"<script>", false},
	}
	for _, tt := range tests {
		if got := ValidRequestID(tt.id); got != tt.want {
			t.Errorf("ValidRequestID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestSecretIsRedacted(t *testing.T) {
	s := Secret("postgres://u:p4ss@db/x")
	var buf bytes.Buffer
	log := New(&buf, slog.LevelInfo, "test")
	log.Info("cfg", "dsn", s)
	// Secret внутри структуры: JSONHandler сериализует её через encoding/json.
	log.Info("cfg", "config", struct{ DSN Secret }{s})
	asJSON, _ := json.Marshal(struct{ DSN Secret }{s})
	for _, out := range []string{buf.String(), string(asJSON), fmt.Sprint(s), fmt.Sprintf("%v %s %#v", s, s, s)} {
		if strings.Contains(out, "p4ss") {
			t.Errorf("secret leaked: %s", out)
		}
	}
	if s.Reveal() != "postgres://u:p4ss@db/x" {
		t.Error("Reveal() changed the value")
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"": slog.LevelInfo, "debug": slog.LevelDebug, "WARN": slog.LevelWarn} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("ParseLevel(loud): want error")
	}
}

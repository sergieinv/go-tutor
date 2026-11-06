package task

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestHandlerKeepsAfterWith(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := WithRequestID(context.Background(), "r-7")
	log.With("svc", "billing").InfoContext(ctx, "счёт")
	want := `{"level":"INFO","msg":"счёт","svc":"billing","request_id":"r-7"}`
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Errorf("после With вывод:\n%s\nwant:\n%s", got, want)
	}
}

func TestHandlerKeepsAfterWithGroup(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := WithRequestID(context.Background(), "r-8")
	log.WithGroup("req").InfoContext(ctx, "ответ", "status", 200)
	want := `{"level":"INFO","msg":"ответ","req":{"status":200,"request_id":"r-8"}}`
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Errorf("после WithGroup вывод:\n%s\nwant:\n%s", got, want)
	}
}

func TestHandlerRespectsLevel(t *testing.T) {
	log, buf := newLogger(slog.LevelWarn)
	ctx := WithRequestID(context.Background(), "r-9")
	log.InfoContext(ctx, "не должно попасть")
	log.DebugContext(ctx, "и это тоже")
	if buf.Len() != 0 {
		t.Errorf("записи ниже уровня Warn выведены:\n%s", buf.String())
	}
	log.WarnContext(ctx, "предупреждение")
	if !strings.Contains(buf.String(), `"request_id":"r-9"`) {
		t.Errorf("запись уровня Warn не выведена или без request_id:\n%s", buf.String())
	}
}

func TestHandlerEmptyIDIgnored(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	log.InfoContext(WithRequestID(context.Background(), ""), "пусто")
	if strings.Contains(buf.String(), "request_id") {
		t.Errorf("пустой идентификатор не должен попадать в лог:\n%s", buf.String())
	}
}

func TestRequestIDNested(t *testing.T) {
	ctx := WithRequestID(context.Background(), "outer")
	ctx = WithRequestID(ctx, "inner")
	if got := RequestID(ctx); got != "inner" {
		t.Errorf("RequestID = %q, want %q — побеждает ближайшее значение", got, "inner")
	}
	// Посторонний ключ со строкой "request_id" не должен совпасть с ключом пакета.
	type foreign string
	other := context.WithValue(context.Background(), foreign("request_id"), "x")
	if got := RequestID(other); got != "" {
		t.Errorf("RequestID нашёл значение по чужому ключу: %q", got)
	}
}

func TestHandlerConcurrent(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			ctx := WithRequestID(context.Background(), fmt.Sprintf("r-%d", i))
			log.InfoContext(ctx, "параллельно")
		})
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 20 {
		t.Fatalf("строк в логе %d, want 20", len(lines))
	}
	for _, l := range lines {
		if strings.Count(l, "request_id") != 1 {
			t.Errorf("в записи должен быть ровно один request_id: %s", l)
		}
	}
}

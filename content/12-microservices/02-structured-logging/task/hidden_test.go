package task

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestWithAttrsAccumulates(t *testing.T) {
	parent := WithAttrs(context.Background(), slog.String("request_id", "r-1"), slog.String("user", "alice"))
	child := WithAttrs(parent, slog.String("tenant", "t-9"), slog.String("user", "bob"))

	got := AttrsFrom(child)
	want := []string{"request_id=r-1", "user=bob", "tenant=t-9"}
	if len(got) != len(want) {
		t.Fatalf("AttrsFrom(child) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("AttrsFrom(child)[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	if p := AttrsFrom(parent); len(p) != 2 || p[1].String() != "user=alice" {
		t.Errorf("родительский контекст изменился: %v", p)
	}
	if a := AttrsFrom(context.Background()); len(a) != 0 {
		t.Errorf("AttrsFrom(пустой) = %v, want пусто", a)
	}
}

func TestAttrsFromReturnsCopy(t *testing.T) {
	ctx := WithAttrs(context.Background(), slog.String("request_id", "r-1"))
	a := AttrsFrom(ctx)
	if len(a) != 1 {
		t.Fatalf("AttrsFrom = %v", a)
	}
	a[0] = slog.String("request_id", "hacked")
	if b := AttrsFrom(ctx); len(b) != 1 || b[0].Value.String() != "r-1" {
		t.Errorf("изменение результата AttrsFrom видно в контексте: %v", b)
	}
}

func TestSiblingContextsIndependent(t *testing.T) {
	// Хвост среза родителя не должен делиться между детьми.
	parent := WithAttrs(context.Background(), slog.String("a", "1"), slog.String("b", "2"))
	parent = WithAttrs(parent, slog.String("c", "3"))
	c1 := WithAttrs(parent, slog.String("x", "1"))
	c2 := WithAttrs(parent, slog.String("y", "2"))
	if a := AttrsFrom(c1); len(a) != 4 || a[3].Key != "x" {
		t.Errorf("AttrsFrom(c1) = %v, want ... x=1", a)
	}
	if a := AttrsFrom(c2); len(a) != 4 || a[3].Key != "y" {
		t.Errorf("AttrsFrom(c2) = %v, want ... y=2", a)
	}
}

func TestWithAndGroupsOrder(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := spanCtx(WithAttrs(context.Background(), slog.String("request_id", "r-1")))
	log.With("service", "orders").WithGroup("db").With("table", "orders").WithGroup("q").
		InfoContext(ctx, "select", "rows", 1)
	check(t, buf, `{"level":"INFO","msg":"select","request_id":"r-1","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","service":"orders","db":{"table":"orders","q":{"rows":1}}}`)
}

func TestEmptyGroupNameIgnored(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := WithAttrs(context.Background(), slog.String("request_id", "r-1"))
	log.WithGroup("").InfoContext(ctx, "m", "k", "v")
	check(t, buf, `{"level":"INFO","msg":"m","request_id":"r-1","k":"v"}`)
}

func TestDerivedLoggersIndependent(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	base := log.With("a", 1).With("b", 2)
	l1 := base.With("c", 3)
	l2 := base.With("d", 4)
	l1.Info("one")
	l2.Info("two")
	base.Info("base")
	check(t, buf,
		`{"level":"INFO","msg":"one","a":1,"b":2,"c":3}`,
		`{"level":"INFO","msg":"two","a":1,"b":2,"d":4}`,
		`{"level":"INFO","msg":"base","a":1,"b":2}`,
	)
}

func TestLevelFromNext(t *testing.T) {
	log, buf := newLogger(slog.LevelWarn)
	ctx := WithAttrs(context.Background(), slog.String("request_id", "r-1"))
	if log.Enabled(ctx, slog.LevelInfo) {
		t.Error("Enabled(Info) = true при уровне next = Warn")
	}
	log.InfoContext(ctx, "hidden")
	log.With("k", 1).WithGroup("g").DebugContext(ctx, "hidden too")
	log.WarnContext(ctx, "shown")
	check(t, buf, `{"level":"WARN","msg":"shown","request_id":"r-1"}`)
}

func TestInvalidSpanContextSkipped(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := trace.ContextWithSpanContext(context.Background(), trace.SpanContext{})
	log.InfoContext(ctx, "m")
	check(t, buf, `{"level":"INFO","msg":"m"}`)
}

type user struct{ id, card string }

func (u user) LogValue() slog.Value { return slog.GroupValue(slog.String("id", u.id)) }

func TestLogValuerInContext(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	ctx := WithAttrs(context.Background(), slog.Any("user", user{"u-7", "4111111111111111"}))
	log.InfoContext(ctx, "pay")
	check(t, buf, `{"level":"INFO","msg":"pay","user":{"id":"u-7"}}`)
}

func TestConcurrentLogging(t *testing.T) {
	log, buf := newLogger(slog.LevelInfo)
	base := log.With("service", "orders")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := base.With("worker", i).WithGroup("g")
			ctx := WithAttrs(context.Background(), slog.Int("n", i))
			for range 10 {
				l.InfoContext(ctx, "tick", "k", 1)
			}
		}()
	}
	wg.Wait()
	got := lines(buf)
	if len(got) != 200 {
		t.Fatalf("строк %d, want 200", len(got))
	}
	for _, l := range got {
		if !strings.HasPrefix(l, `{"level":"INFO","msg":"tick","n":`) || !strings.HasSuffix(l, `"g":{"k":1}}`) {
			t.Fatalf("строка испорчена: %s", l)
		}
	}
}

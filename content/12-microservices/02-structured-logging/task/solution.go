package task

import (
	"context"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/trace"
)

type attrsKey struct{}

// WithAttrs возвращает контекст с добавленными атрибутами запроса.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	old, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	out := slices.Clone(old)
	for _, a := range attrs {
		if i := slices.IndexFunc(out, func(b slog.Attr) bool { return b.Key == a.Key }); i >= 0 {
			out[i] = a
			continue
		}
		out = append(out, a)
	}
	return context.WithValue(ctx, attrsKey{}, out)
}

// AttrsFrom возвращает копию атрибутов запроса из контекста.
func AttrsFrom(ctx context.Context) []slog.Attr {
	as, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	return slices.Clone(as)
}

// groupOrAttrs — один вызов WithGroup (group != "") или WithAttrs.
type groupOrAttrs struct {
	group string
	attrs []slog.Attr
}

type contextHandler struct {
	next slog.Handler
	goas []groupOrAttrs
}

// NewContextHandler оборачивает next.
func NewContextHandler(next slog.Handler) slog.Handler {
	return &contextHandler{next: next}
}

func (h *contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *contextHandler) with(g groupOrAttrs) *contextHandler {
	return &contextHandler{next: h.next, goas: append(slices.Clip(h.goas), g)}
}

func (h *contextHandler) WithAttrs(as []slog.Attr) slog.Handler {
	if len(as) == 0 {
		return h
	}
	return h.with(groupOrAttrs{attrs: slices.Clone(as)})
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.with(groupOrAttrs{group: name})
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	// Атрибуты вызова — в самой внутренней группе.
	inner := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		inner = append(inner, a)
		return true
	})
	// Сворачиваем With и WithGroup изнутри наружу.
	for i := len(h.goas) - 1; i >= 0; i-- {
		g := h.goas[i]
		if g.group != "" {
			inner = []slog.Attr{{Key: g.group, Value: slog.GroupValue(inner...)}}
			continue
		}
		inner = append(slices.Clone(g.attrs), inner...)
	}

	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(AttrsFrom(ctx)...)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		out.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	out.AddAttrs(inner...)
	return h.next.Handle(ctx, out)
}

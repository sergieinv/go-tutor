package task

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// binProto — «Minecraft»: без событий пачка пустая, и её не шлют.
type binProto struct {
	name    string
	skip    map[string]bool // события, которые протоколу не нужны
	begins  int
	appends int
	ends    int
	stats   []Stats
}

func (p *binProto) Begin(dst []byte) []byte { p.begins++; return dst }

func (p *binProto) Append(dst []byte, ev *Event) []byte {
	p.appends++
	if p.skip[ev.Kind] {
		return dst
	}
	return fmt.Appendf(dst, "%s:%s:%d:%s;", p.name, ev.Kind, ev.Subject, ev.Text)
}

func (p *binProto) End(dst []byte, st Stats) []byte {
	p.ends++
	p.stats = append(p.stats, st)
	return dst
}

// jsonProto — «браузер»: пачка уходит каждый тик, последним — tick.
type jsonProto struct {
	begins  int
	appends int
	ends    int
}

func (p *jsonProto) Begin(dst []byte) []byte { p.begins++; return append(dst, '[') }

func (p *jsonProto) Append(dst []byte, ev *Event) []byte {
	p.appends++
	return fmt.Appendf(dst, `{"t":%q,"id":%d,"text":%q},`, ev.Kind, ev.Subject, ev.Text)
}

func (p *jsonProto) End(dst []byte, st Stats) []byte {
	p.ends++
	return fmt.Appendf(dst, `{"t":"tick","n":%d,"msgs":%d}]`, st.Tick, st.Events)
}

func mustAdd(t *testing.T, h *Hub, id PlayerID, p Protocol) {
	t.Helper()
	if err := h.Add(id, p); err != nil {
		t.Fatalf("Add(%d): %v", id, err)
	}
}

func mustSee(t *testing.T, h *Hub, a, b PlayerID) {
	t.Helper()
	if err := h.SetVisible(a, b, true); err != nil {
		t.Fatalf("SetVisible(%d, %d): %v", a, b, err)
	}
}

// render — пачки строкой «id=данные» через пробел, для сравнения.
func render(bs []Batch) string {
	var parts []string
	for _, b := range bs {
		parts = append(parts, fmt.Sprintf("%d=%s", b.Player, b.Data))
	}
	return strings.Join(parts, " ")
}

func TestAddErrors(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	if err := h.Add(1, mc); err != nil {
		t.Fatalf("Add(1) = %v", err)
	}
	if err := h.Add(1, mc); !errors.Is(err, ErrDuplicate) {
		t.Errorf("повторный Add(1) = %v, want ErrDuplicate", err)
	}
	if err := h.Add(0, mc); !errors.Is(err, ErrBadPlayer) {
		t.Errorf("Add(0) = %v, want ErrBadPlayer", err)
	}
	if err := h.Add(2, nil); !errors.Is(err, ErrBadPlayer) {
		t.Errorf("Add(2, nil) = %v, want ErrBadPlayer", err)
	}
	if err := h.SetVisible(1, 9, true); !errors.Is(err, ErrUnknownPlayer) {
		t.Errorf("SetVisible(1, 9) = %v, want ErrUnknownPlayer", err)
	}
}

func TestToAllEncodesOncePerProtocol(t *testing.T) {
	h := NewHub()
	mc, web := &binProto{name: "mc"}, &jsonProto{}
	for id := PlayerID(1); id <= 3; id++ {
		mustAdd(t, h, id, mc)
	}
	mustAdd(t, h, 4, web)
	mustAdd(t, h, 5, web)

	h.Emit(Item{Event: Event{Kind: "join", Subject: 5, Text: "Eve"}, To: ToAll})
	got := render(h.Flush(7))
	want := `1=mc:join:5:Eve; 2=mc:join:5:Eve; 3=mc:join:5:Eve; ` +
		`4=[{"t":"join","id":5,"text":"Eve"},{"t":"tick","n":7,"msgs":1}] ` +
		`5=[{"t":"join","id":5,"text":"Eve"},{"t":"tick","n":7,"msgs":1}]`
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	if mc.appends != 1 || web.appends != 1 {
		t.Errorf("Append вызван mc=%d web=%d раз, want по одному на протокол", mc.appends, web.appends)
	}
}

func TestViewersAndSelf(t *testing.T) {
	h := NewHub()
	mc, web := &binProto{name: "mc"}, &jsonProto{}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	mustAdd(t, h, 3, web)
	mustAdd(t, h, 4, mc) // никого не видит
	mustSee(t, h, 1, 2)
	mustSee(t, h, 1, 3)

	h.Emit(Item{Event: Event{Kind: "move", Subject: 1}, To: ToViewers})
	h.Emit(Item{Event: Event{Kind: "chat", Subject: 1, Text: "hi"}, To: ToViewers, Self: true})
	got := render(h.Flush(1))
	want := `1=mc:chat:1:hi; 2=mc:move:1:;mc:chat:1:hi; ` +
		`3=[{"t":"move","id":1,"text":""},{"t":"chat","id":1,"text":"hi"},{"t":"tick","n":1,"msgs":2}]`
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
}

func TestOneAndEmptyBatches(t *testing.T) {
	h := NewHub()
	mc, web := &binProto{name: "mc"}, &jsonProto{}
	mustAdd(t, h, 2, mc)
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 3, web)

	h.Emit(Item{Event: Event{Kind: "welcome", Subject: 1}, To: ToOne, Target: 1})
	got := render(h.Flush(5))
	want := `1=mc:welcome:1:; 3=[{"t":"tick","n":5,"msgs":0}]`
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	if mc.begins != 2 || mc.ends != 2 || web.begins != 1 || web.ends != 1 {
		t.Errorf("Begin/End: mc %d/%d, web %d/%d; want по одному на игрока",
			mc.begins, mc.ends, web.begins, web.ends)
	}
	if got := render(h.Flush(6)); got != `3=[{"t":"tick","n":6,"msgs":0}]` {
		t.Errorf("второй Flush = %s: события должны уходить один раз", got)
	}
}

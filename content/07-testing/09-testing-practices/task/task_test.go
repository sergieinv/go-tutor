package task

import (
	"errors"
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)

func newTestDesk(t *testing.T) (*Desk, *FakeClock) {
	t.Helper()
	clock := NewFakeClock(t0)
	return NewDesk(clock.Now, SeqIDs("T")), clock
}

func ids(list []Ticket) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.ID)
	}
	return out
}

func TestOpen(t *testing.T) {
	d, clock := newTestDesk(t)
	got, err := d.Open("  принтер не печатает ", 2)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := Ticket{ID: "T-1", Title: "принтер не печатает", Priority: 2, Created: t0}
	if got != want {
		t.Errorf("Open = %+v, want %+v", got, want)
	}
	clock.Advance(time.Minute)
	got, err = d.Open("VPN", 3)
	want = Ticket{ID: "T-2", Title: "VPN", Priority: 3, Created: t0.Add(time.Minute)}
	if err != nil || got != want {
		t.Errorf("второй Open = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestOpenInvalid(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		priority int
	}{
		{"пустой заголовок", "", 1},
		{"одни пробелы", " \t ", 1},
		{"приоритет 0", "диск", 0},
		{"приоритет 4", "диск", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := newTestDesk(t)
			got, err := d.Open(tt.title, tt.priority)
			if !errors.Is(err, ErrInvalid) || got != (Ticket{}) {
				t.Errorf("Open(%q, %d) = %+v, %v; want нулевой Ticket и ErrInvalid", tt.title, tt.priority, got, err)
			}
		})
	}
}

func TestQueueOrder(t *testing.T) {
	d, clock := newTestDesk(t)
	d.Open("a", 1) // T-1
	clock.Advance(time.Second)
	d.Open("b", 3) // T-2
	d.Open("c", 1) // T-3, то же время, что у T-2
	clock.Advance(time.Second)
	d.Open("d", 3) // T-4
	want := []string{"T-2", "T-4", "T-1", "T-3"}
	if got := ids(d.Queue()); !slices.Equal(got, want) {
		t.Errorf("Queue() = %v, want %v", got, want)
	}
}

func TestExpire(t *testing.T) {
	d, clock := newTestDesk(t)
	d.Open("старый", 1) // T-1
	clock.Advance(30 * time.Minute)
	d.Open("новый", 1) // T-2
	clock.Advance(30 * time.Minute)
	if n := d.Expire(time.Hour); n != 1 {
		t.Errorf("Expire(1h) = %d, want 1 (ровно час — уже просрочен)", n)
	}
	if got, want := ids(d.Queue()), []string{"T-2"}; !slices.Equal(got, want) {
		t.Errorf("после Expire Queue() = %v, want %v", got, want)
	}
}

func TestSeqIDs(t *testing.T) {
	a, b := SeqIDs("A"), SeqIDs("A")
	got := []string{a(), a(), b(), a()}
	want := []string{"A-1", "A-2", "A-1", "A-3"}
	if !slices.Equal(got, want) {
		t.Errorf("SeqIDs = %v, want %v", got, want)
	}
}

package task

import (
	"cmp"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Ticket struct {
	ID       string
	Title    string
	Priority int
	Created  time.Time
}

var ErrInvalid = errors.New("desk: неверный тикет")

// Desk — очередь тикетов. Всё состояние — в экземпляре,
// зависимости приходят через конструктор.
type Desk struct {
	now   func() time.Time
	newID func() string

	mu      sync.Mutex
	tickets map[string]Ticket
}

func NewDesk(now func() time.Time, newID func() string) *Desk {
	if now == nil {
		now = time.Now
	}
	if newID == nil {
		newID = rand.Text
	}
	return &Desk{now: now, newID: newID, tickets: map[string]Ticket{}}
}

func (d *Desk) Open(title string, priority int) (Ticket, error) {
	title = strings.TrimSpace(title)
	if title == "" || priority < 1 || priority > 3 {
		return Ticket{}, fmt.Errorf("%w: заголовок %q, приоритет %d", ErrInvalid, title, priority)
	}
	t := Ticket{ID: d.newID(), Title: title, Priority: priority, Created: d.now()}
	d.mu.Lock()
	d.tickets[t.ID] = t
	d.mu.Unlock()
	return t, nil
}

func (d *Desk) Queue() []Ticket {
	d.mu.Lock()
	list := make([]Ticket, 0, len(d.tickets))
	for _, t := range d.tickets {
		list = append(list, t)
	}
	d.mu.Unlock()
	slices.SortFunc(list, func(a, b Ticket) int {
		return cmp.Or(
			cmp.Compare(b.Priority, a.Priority),
			a.Created.Compare(b.Created),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return list
}

func (d *Desk) Expire(maxAge time.Duration) int {
	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for id, t := range d.tickets {
		if now.Sub(t.Created) >= maxAge {
			delete(d.tickets, id)
			n++
		}
	}
	return n
}

// FakeClock — часы, которые двигает только тест.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: start}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// SeqIDs — генератор предсказуемых ID со своим счётчиком.
func SeqIDs(prefix string) func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		n++
		id := n
		mu.Unlock()
		return prefix + "-" + strconv.Itoa(id)
	}
}

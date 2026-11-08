package task

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Работающий, но нетестируемый код: время из time.Now,
// пакетный счётчик и пакетная map, порядок очереди зависит
// от обхода map, синхронизации нет. Перепишите его по
// условию задания, сохранив сигнатуры.

type Ticket struct {
	ID       string
	Title    string
	Priority int
	Created  time.Time
}

var ErrInvalid = errors.New("desk: неверный тикет")

var (
	tickets = map[string]Ticket{}
	seq     int
)

type Desk struct{}

func NewDesk(now func() time.Time, newID func() string) *Desk {
	return &Desk{} // зависимости игнорируются
}

func (d *Desk) Open(title string, priority int) (Ticket, error) {
	if strings.TrimSpace(title) == "" || priority < 1 || priority > 3 {
		return Ticket{}, ErrInvalid
	}
	seq++
	t := Ticket{ID: fmt.Sprintf("T-%d", seq), Title: strings.TrimSpace(title), Priority: priority, Created: time.Now()}
	tickets[t.ID] = t
	return t, nil
}

func (d *Desk) Queue() []Ticket {
	var list []Ticket
	for _, t := range tickets {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Priority > list[j].Priority })
	return list
}

func (d *Desk) Expire(maxAge time.Duration) int {
	n := 0
	for id, t := range tickets {
		if time.Since(t.Created) > maxAge {
			delete(tickets, id)
			n++
		}
	}
	return n
}

type FakeClock struct{}

func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{} }

func (c *FakeClock) Now() time.Time { return time.Now() }

func (c *FakeClock) Advance(d time.Duration) {}

var idSeq int

func SeqIDs(prefix string) func() string {
	return func() string {
		idSeq++
		return fmt.Sprintf("%s-%d", prefix, idSeq)
	}
}

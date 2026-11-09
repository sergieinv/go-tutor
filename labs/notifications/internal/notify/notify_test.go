package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	eventsv1 "gotutor/labs/api/gen/events/v1"
)

// memStore — хранилище в памяти: достаточно, чтобы проверить логику
// обработчика без базы. Настоящую транзакционность проверяют контрактные
// тесты пакета sqlite.
type memStore struct {
	seen  map[string]bool
	saved []Notification
	err   error
}

func (s *memStore) SaveOnce(_ context.Context, n Notification) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if s.seen[n.EventID] {
		return false, nil
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[n.EventID] = true
	s.saved = append(s.saved, n)
	return true, nil
}

var testNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func newHandler(s Store) *Handler {
	return NewHandler(s, func() time.Time { return testNow }, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func created(eventID string, orderID, customerID, totalCents int64) *eventsv1.OrderEvent {
	return &eventsv1.OrderEvent{
		EventId: eventID,
		OrderId: orderID,
		Event: &eventsv1.OrderEvent_OrderCreated{OrderCreated: &eventsv1.OrderCreated{
			CustomerId: customerID, TotalCents: totalCents,
		}},
	}
}

func TestHandle(t *testing.T) {
	cancelled := &eventsv1.OrderEvent{
		EventId: "e2", OrderId: 42,
		Event: &eventsv1.OrderEvent_OrderCancelled{OrderCancelled: &eventsv1.OrderCancelled{CustomerId: 5, TotalCents: 598000}},
	}
	tests := []struct {
		name string
		ev   *eventsv1.OrderEvent
		want Outcome
		text string
	}{
		{"created", created("e1", 42, 5, 598000),
			Outcome{EventType: EventOrderCreated, Result: ResultSent, Kind: KindOrderCreated},
			"Заказ 42 оформлен на 5 980,00 ₽"},
		{"cancelled", cancelled,
			Outcome{EventType: EventOrderCancelled, Result: ResultSent, Kind: KindOrderCancelled},
			"Заказ 42 отменён"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &memStore{}
			got, err := newHandler(s).Handle(t.Context(), tt.ev)
			if err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if got != tt.want {
				t.Errorf("Handle = %+v, want %+v", got, tt.want)
			}
			if len(s.saved) != 1 {
				t.Fatalf("saved %d notifications, want 1", len(s.saved))
			}
			want := Notification{EventID: tt.ev.GetEventId(), OrderID: 42, CustomerID: 5, Kind: tt.want.Kind, Text: tt.text, CreatedAt: testNow}
			if s.saved[0] != want {
				t.Errorf("saved %+v, want %+v", s.saved[0], want)
			}
		})
	}
}

func TestHandleDuplicate(t *testing.T) {
	s := &memStore{}
	h := newHandler(s)
	ev := created("e1", 1, 2, 100)
	if _, err := h.Handle(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	got, err := h.Handle(t.Context(), ev)
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != ResultDuplicate || len(s.saved) != 1 {
		t.Errorf("second Handle = %+v with %d saved, want duplicate and 1 saved", got, len(s.saved))
	}
}

func TestHandleUnknownEventIsSkipped(t *testing.T) {
	s := &memStore{}
	got, err := newHandler(s).Handle(t.Context(), &eventsv1.OrderEvent{EventId: "e1", OrderId: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := (Outcome{EventType: EventUnknown, Result: ResultSkipped}); got != want || len(s.saved) != 0 {
		t.Errorf("Handle = %+v with %d saved, want %+v and nothing saved", got, len(s.saved), want)
	}
}

func TestHandleStoreError(t *testing.T) {
	boom := errors.New("database is locked")
	_, err := newHandler(&memStore{err: boom}).Handle(t.Context(), created("e1", 1, 2, 100))
	if !errors.Is(err, boom) {
		t.Errorf("Handle error = %v, want %v", err, boom)
	}
}

func TestDecode(t *testing.T) {
	good, err := proto.Marshal(created("e1", 42, 5, 100))
	if err != nil {
		t.Fatal(err)
	}
	noID, err := proto.Marshal(&eventsv1.OrderEvent{OrderId: 1})
	if err != nil {
		t.Fatal(err)
	}

	ev, err := Decode(good)
	if err != nil || ev.GetEventId() != "e1" || ev.GetOrderCreated().GetCustomerId() != 5 {
		t.Errorf("Decode(good) = %v, %v", ev, err)
	}
	for name, value := range map[string][]byte{
		"garbage":  []byte("not a protobuf"),
		"no id":    noID,
		"empty":    nil,
		"truncate": good[:len(good)-1],
	} {
		if _, err := Decode(value); !errors.Is(err, ErrInvalidEvent) {
			t.Errorf("Decode(%s) error = %v, want ErrInvalidEvent", name, err)
		}
	}
}

func TestFormatRubles(t *testing.T) {
	tests := []struct {
		cents int64
		want  string
	}{
		{0, "0,00 ₽"},
		{5, "0,05 ₽"},
		{99900, "999,00 ₽"},
		{100000, "1 000,00 ₽"},
		{598000, "5 980,00 ₽"},
		{123456789, "1 234 567,89 ₽"},
		{-150, "-1,50 ₽"},
		{math.MinInt64, "-92 233 720 368 547 758,08 ₽"},
	}
	for _, tt := range tests {
		if got := FormatRubles(tt.cents); got != tt.want {
			t.Errorf("FormatRubles(%d) = %q, want %q", tt.cents, got, tt.want)
		}
	}
}

package orders

import (
	"context"
	"crypto/rand"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "gotutor/labs/api/gen/events/v1"
)

// addEvent пишет в outbox событие о текущем состоянии заказа: OrderCreated
// для зарезервированного, OrderCancelled для отменённого.
//
// Событие строится из контракта events.v1, а не из доменной структуры:
// формат события — обещание потребителям, он меняется только по правилам
// эволюции protobuf, а домен можно менять свободно.
func (s *Service) addEvent(ctx context.Context, tx Tx, o Order) error {
	ev := &eventsv1.OrderEvent{
		// 128 случайных бит: id уникален глобально, без координации.
		EventId:   rand.Text(),
		OccurTime: timestamppb.New(o.UpdatedAt),
		OrderId:   o.ID,
	}
	var eventType string
	switch o.Status {
	case StatusReserved:
		eventType = EventOrderCreated
		items := make([]*eventsv1.OrderItem, len(o.Items))
		for i, it := range o.Items {
			items[i] = &eventsv1.OrderItem{ProductId: it.ProductID, Quantity: int32(it.Quantity), PriceCents: it.PriceCents}
		}
		ev.Event = &eventsv1.OrderEvent_OrderCreated{OrderCreated: &eventsv1.OrderCreated{
			CustomerId: o.CustomerID, TotalCents: o.TotalCents, Items: items,
		}}
	case StatusCancelled:
		eventType = EventOrderCancelled
		ev.Event = &eventsv1.OrderEvent_OrderCancelled{OrderCancelled: &eventsv1.OrderCancelled{
			CustomerId: o.CustomerID, TotalCents: o.TotalCents,
		}}
	default:
		return fmt.Errorf("no event for order status %q", o.Status)
	}
	payload, err := proto.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", eventType, err)
	}
	// Контекст трассы запроса — в строку outbox: relay опубликует событие
	// позже и продолжит ту же трассу. API OpenTelemetry без настроенного
	// SDK ничего не делает, так что домен от трассировки не зависит.
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return tx.AddEvent(ctx, Event{
		EventID:      ev.GetEventId(),
		Type:         eventType,
		OrderID:      o.ID,
		Payload:      payload,
		TraceContext: carrier,
		CreatedAt:    o.UpdatedAt,
	})
}

package task

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// Line — позиция заказа.
type Line struct {
	SKU string
	Qty int
}

// Order — заказ, как его хранят «Заказы».
type Order struct {
	ID       string
	Customer string
	Lines    []Line
}

// Event — событие, которое «Заказы» публикуют для других сервисов.
type Event struct {
	Type     string
	OrderID  string
	Customer string
}

var (
	ErrInvalid    = errors.New("invalid order")
	ErrOutOfStock = errors.New("out of stock")
)

// Stock — клиент «Склада» глазами «Заказов».
type Stock interface {
	Reserve(ctx context.Context, orderID string, lines []Line) error
	Release(ctx context.Context, orderID string) error
}

// Publisher — отправка событий (в системе — продюсер Kafka).
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// Service — сервис «Заказы».
type Service struct {
	stock  Stock
	events Publisher
	newID  func() string

	mu     sync.Mutex
	orders map[string]Order
}

func NewService(stock Stock, events Publisher, newID func() string) *Service {
	return &Service{stock: stock, events: events, newID: newID, orders: map[string]Order{}}
}

// normalize проверяет позиции, складывает одинаковые SKU и сортирует по SKU.
func normalize(lines []Line) ([]Line, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("no lines: %w", ErrInvalid)
	}
	sum := map[string]int{}
	for _, l := range lines {
		if l.SKU == "" || l.Qty <= 0 {
			return nil, fmt.Errorf("line %+v: %w", l, ErrInvalid)
		}
		sum[l.SKU] += l.Qty
	}
	out := make([]Line, 0, len(sum))
	for sku, n := range sum {
		out = append(out, Line{SKU: sku, Qty: n})
	}
	slices.SortFunc(out, func(a, b Line) int { return cmp.Compare(a.SKU, b.SKU) })
	return out, nil
}

func (s *Service) PlaceOrder(ctx context.Context, customer string, lines []Line) (Order, error) {
	if customer == "" {
		return Order{}, fmt.Errorf("empty customer: %w", ErrInvalid)
	}
	norm, err := normalize(lines)
	if err != nil {
		return Order{}, err
	}
	id := s.newID()
	if err := s.stock.Reserve(ctx, id, norm); err != nil {
		return Order{}, fmt.Errorf("reserve %s: %w", id, err)
	}
	ev := Event{Type: "order.created", OrderID: id, Customer: customer}
	if err := s.events.Publish(ctx, ev); err != nil {
		// Компенсация: резерв без заказа никому не нужен. Отмена
		// запроса не должна её сорвать — отсюда WithoutCancel.
		if rerr := s.stock.Release(context.WithoutCancel(ctx), id); rerr != nil {
			return Order{}, errors.Join(fmt.Errorf("publish %s: %w", id, err), fmt.Errorf("release %s: %w", id, rerr))
		}
		return Order{}, fmt.Errorf("publish %s: %w", id, err)
	}
	o := Order{ID: id, Customer: customer, Lines: norm}
	s.mu.Lock()
	s.orders[id] = o
	s.mu.Unlock()
	return cloneOrder(o), nil
}

func (s *Service) Get(id string) (Order, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[id]
	if !ok {
		return Order{}, false
	}
	return cloneOrder(o), true
}

func cloneOrder(o Order) Order {
	o.Lines = slices.Clone(o.Lines)
	return o
}

// MemStock — фейк «Склада» в памяти.
type MemStock struct {
	mu       sync.Mutex
	stock    map[string]int
	reserved map[string][]Line
}

func NewMemStock(stock map[string]int) *MemStock {
	m := &MemStock{stock: map[string]int{}, reserved: map[string][]Line{}}
	for sku, n := range stock {
		m.stock[sku] = n
	}
	return m
}

func (m *MemStock) Reserve(ctx context.Context, orderID string, lines []Line) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	need := map[string]int{}
	for _, l := range lines {
		if l.Qty <= 0 {
			return fmt.Errorf("line %+v: %w", l, ErrInvalid)
		}
		need[l.SKU] += l.Qty
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.reserved[orderID]; ok {
		return nil // повтор того же резерва
	}
	for sku, n := range need {
		if m.stock[sku] < n {
			return fmt.Errorf("%s: %w", sku, ErrOutOfStock)
		}
	}
	var held []Line
	for sku, n := range need {
		m.stock[sku] -= n
		held = append(held, Line{SKU: sku, Qty: n})
	}
	m.reserved[orderID] = held
	return nil
}

func (m *MemStock) Release(ctx context.Context, orderID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.reserved[orderID] {
		m.stock[l.SKU] += l.Qty
	}
	delete(m.reserved, orderID)
	return nil
}

func (m *MemStock) Available(sku string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stock[sku]
}

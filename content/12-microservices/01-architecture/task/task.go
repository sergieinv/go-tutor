package task

import (
	"context"
	"errors"
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
	// TODO: зависимости и хранилище заказов
}

func NewService(stock Stock, events Publisher, newID func() string) *Service {
	// TODO
	return &Service{}
}

// PlaceOrder — бывшая монолитная функция, теперь через клиентов соседей.
func (s *Service) PlaceOrder(ctx context.Context, customer string, lines []Line) (Order, error) {
	// TODO
	return Order{}, nil
}

func (s *Service) Get(id string) (Order, bool) {
	// TODO
	return Order{}, false
}

// MemStock — фейк «Склада» в памяти.
type MemStock struct {
	// TODO
}

func NewMemStock(stock map[string]int) *MemStock {
	// TODO
	return &MemStock{}
}

func (m *MemStock) Reserve(ctx context.Context, orderID string, lines []Line) error {
	// TODO
	return nil
}

func (m *MemStock) Release(ctx context.Context, orderID string) error {
	// TODO
	return nil
}

func (m *MemStock) Available(sku string) int {
	// TODO
	return 0
}

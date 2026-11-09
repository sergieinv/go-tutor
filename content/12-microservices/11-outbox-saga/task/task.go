package task

import (
	"context"
	"errors"
)

// Status — состояние саги.
type Status string

const (
	StatusRunning      Status = "running"      // шаги выполняются
	StatusCompleted    Status = "completed"    // все шаги выполнены
	StatusCompensating Status = "compensating" // шаг упал, идут компенсации
	StatusCompensated  Status = "compensated"  // все выполненные шаги компенсированы
	StatusFailed       Status = "failed"       // компенсация не удалась — нужен человек
)

// ErrUnknownStatus — в хранилище статус, которого оркестратор не знает.
var ErrUnknownStatus = errors.New("сага: неизвестный статус")

// Step — шаг саги в чужом сервисе. Do и Compensate идемпотентны:
// после сбоя их могут вызвать повторно.
type Step struct {
	Name       string
	Do         func(ctx context.Context, sagaID string) error
	Compensate func(ctx context.Context, sagaID string) error // nil — компенсировать нечего
}

// State — сохранённое состояние саги.
type State struct {
	Status Status
	Done   []string // выполненные и ещё не компенсированные шаги, по порядку
}

// Store хранит состояние саг (в сервисе — таблица в БД).
type Store interface {
	// Load возвращает State{} и nil, если саги ещё нет.
	Load(ctx context.Context, sagaID string) (State, error)
	Save(ctx context.Context, sagaID string, s State) error
}

// Run выполняет или продолжает сагу sagaID.
func Run(ctx context.Context, store Store, sagaID string, steps []Step) (Status, error) {
	// TODO
	return "", errors.New("не реализовано")
}

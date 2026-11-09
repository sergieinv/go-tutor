package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	st, err := store.Load(ctx, sagaID)
	if err != nil {
		return "", fmt.Errorf("сага %s: загрузка: %w", sagaID, err)
	}
	save := func() error {
		if err := store.Save(ctx, sagaID, State{Status: st.Status, Done: slices.Clone(st.Done)}); err != nil {
			return fmt.Errorf("сага %s: сохранение: %w", sagaID, err)
		}
		return nil
	}

	switch st.Status {
	case StatusCompleted, StatusCompensated:
		return st.Status, nil // повторный запуск ничего не делает
	case "", StatusRunning:
		var stepErr error
		for _, s := range steps {
			if slices.Contains(st.Done, s.Name) {
				continue // выполнен до перезапуска
			}
			if err := s.Do(ctx, sagaID); err != nil {
				stepErr = fmt.Errorf("шаг %s: %w", s.Name, err)
				break
			}
			st.Status = StatusRunning
			st.Done = append(st.Done, s.Name)
			if err := save(); err != nil {
				return "", err
			}
		}
		if stepErr == nil {
			st.Status = StatusCompleted
			if err := save(); err != nil {
				return "", err
			}
			return StatusCompleted, nil
		}
		st.Status = StatusCompensating
		if err := save(); err != nil {
			return "", errors.Join(stepErr, err)
		}
		return compensate(ctx, &st, save, sagaID, steps, stepErr)
	case StatusCompensating, StatusFailed:
		return compensate(ctx, &st, save, sagaID, steps, nil)
	default:
		// Испорченное или чужое состояние не должно запускать откат.
		return "", fmt.Errorf("сага %s: %q: %w", sagaID, st.Status, ErrUnknownStatus)
	}
}

// compensate откатывает выполненные шаги в обратном порядке.
func compensate(ctx context.Context, st *State, save func() error, sagaID string, steps []Step, cause error) (Status, error) {
	byName := map[string]Step{}
	for _, s := range steps {
		byName[s.Name] = s
	}
	st.Status = StatusCompensating
	for len(st.Done) > 0 {
		name := st.Done[len(st.Done)-1]
		s, ok := byName[name]
		if !ok {
			return "", fmt.Errorf("сага %s: неизвестный шаг %q", sagaID, name)
		}
		if s.Compensate != nil {
			if err := s.Compensate(ctx, sagaID); err != nil {
				st.Status = StatusFailed
				err = fmt.Errorf("компенсация %s: %w", name, err)
				if serr := save(); serr != nil {
					return "", errors.Join(cause, err, serr)
				}
				return StatusFailed, errors.Join(cause, err)
			}
		}
		st.Done = st.Done[:len(st.Done)-1]
		if err := save(); err != nil {
			return "", errors.Join(cause, err)
		}
	}
	st.Status = StatusCompensated
	if err := save(); err != nil {
		return "", errors.Join(cause, err)
	}
	return StatusCompensated, cause
}

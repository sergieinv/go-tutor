package task

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// memStore — хранилище состояний в памяти. failSave — номер вызова
// Save (с 1), который вернёт ошибку; 0 — не падать.
type memStore struct {
	states   map[string]State
	saves    int
	failSave int
	loadErr  error
}

var errSave = errors.New("БД недоступна")

func newStore() *memStore { return &memStore{states: map[string]State{}} }

func (s *memStore) Load(_ context.Context, id string) (State, error) {
	if s.loadErr != nil {
		return State{}, s.loadErr
	}
	st := s.states[id]
	return State{Status: st.Status, Done: slices.Clone(st.Done)}, nil
}

func (s *memStore) Save(_ context.Context, id string, st State) error {
	s.saves++
	if s.saves == s.failSave {
		return errSave
	}
	s.states[id] = State{Status: st.Status, Done: slices.Clone(st.Done)}
	return nil
}

// journal записывает вызовы шагов: "do:reserve", "undo:reserve".
type journal struct {
	calls []string
	fail  map[string]error // ключ — "do:имя" или "undo:имя"
}

func (j *journal) step(name string) Step {
	return Step{
		Name: name,
		Do: func(_ context.Context, sagaID string) error {
			j.calls = append(j.calls, "do:"+name)
			return j.fail["do:"+name]
		},
		Compensate: func(_ context.Context, sagaID string) error {
			j.calls = append(j.calls, "undo:"+name)
			return j.fail["undo:"+name]
		},
	}
}

func (j *journal) steps(names ...string) []Step {
	var out []Step
	for _, n := range names {
		out = append(out, j.step(n))
	}
	return out
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestCompleted(t *testing.T) {
	store := newStore()
	j := &journal{}
	st, err := Run(context.Background(), store, "s1", j.steps("reserve", "order", "pay"))
	if st != StatusCompleted || err != nil {
		t.Fatalf("Run = %q, %v; want completed, nil", st, err)
	}
	eq(t, "вызовы", j.calls, []string{"do:reserve", "do:order", "do:pay"})
	eq(t, "состояние", fmt.Sprint(store.states["s1"]), fmt.Sprint(State{StatusCompleted, []string{"reserve", "order", "pay"}}))

	// Повторный запуск завершённой саги ничего не делает.
	j.calls = nil
	st, err = Run(context.Background(), store, "s1", j.steps("reserve", "order", "pay"))
	if st != StatusCompleted || err != nil || len(j.calls) != 0 {
		t.Fatalf("повтор: %q, %v, вызовы %v; want completed, nil, нет вызовов", st, err, j.calls)
	}
}

func TestCompensatedInReverse(t *testing.T) {
	errDeclined := errors.New("карта отклонена")
	store := newStore()
	j := &journal{fail: map[string]error{"do:pay": errDeclined}}
	st, err := Run(context.Background(), store, "s2", j.steps("reserve", "order", "pay"))
	if st != StatusCompensated || !errors.Is(err, errDeclined) {
		t.Fatalf("Run = %q, %v; want compensated и ошибку шага", st, err)
	}
	eq(t, "вызовы", j.calls, []string{"do:reserve", "do:order", "do:pay", "undo:order", "undo:reserve"})
	if s := store.states["s2"]; s.Status != StatusCompensated || len(s.Done) != 0 {
		t.Errorf("состояние %v, want compensated без выполненных шагов", s)
	}
}

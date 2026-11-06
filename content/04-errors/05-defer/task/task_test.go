package task

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

// fake — ресурс, который пишет события в общий журнал.
type fake struct {
	name     string
	env      *env
	useErr   error
	closeErr error
}

func (f *fake) Use() error {
	f.env.log = append(f.env.log, "use "+f.name)
	return f.useErr
}

func (f *fake) Close() error {
	f.env.log = append(f.env.log, "close "+f.name)
	f.env.open--
	return f.closeErr
}

type env struct {
	log      []string
	open     int // открыто сейчас
	maxOpen  int // максимум одновременно открытых
	openErr  map[string]error
	useErr   map[string]error
	closeErr map[string]error
}

func (e *env) opener(name string) (Resource, error) {
	e.log = append(e.log, "open "+name)
	if err := e.openErr[name]; err != nil {
		return nil, err
	}
	e.open++
	e.maxOpen = max(e.maxOpen, e.open)
	return &fake{name: name, env: e, useErr: e.useErr[name], closeErr: e.closeErr[name]}, nil
}

func TestUseAllOrder(t *testing.T) {
	e := &env{}
	if err := UseAll(e.opener, []string{"a", "b"}); err != nil {
		t.Fatalf("UseAll вернул %v", err)
	}
	want := []string{"open a", "use a", "close a", "open b", "use b", "close b"}
	if !slices.Equal(e.log, want) {
		t.Errorf("журнал = %v, want %v", e.log, want)
	}
	if e.maxOpen > 1 {
		t.Errorf("одновременно открыто %d ресурсов, want не больше 1", e.maxOpen)
	}
}

func TestUseAllUseError(t *testing.T) {
	boom := errors.New("boom")
	e := &env{useErr: map[string]error{"a": boom}}
	err := UseAll(e.opener, []string{"a", "b"})
	if !errors.Is(err, boom) {
		t.Fatalf("UseAll вернул %v, want ошибку Use", err)
	}
	want := []string{"open a", "use a", "close a"}
	if !slices.Equal(e.log, want) {
		t.Errorf("журнал = %v, want %v", e.log, want)
	}
	if got := fmt.Sprint(err); got != "a: boom" {
		t.Errorf("текст ошибки = %q, want %q", got, "a: boom")
	}
}

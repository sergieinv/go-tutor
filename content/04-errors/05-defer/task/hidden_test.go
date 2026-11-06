package task

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestUseAllCloseError(t *testing.T) {
	disk := errors.New("disk full")
	e := &env{closeErr: map[string]error{"b": disk}}
	err := UseAll(e.opener, []string{"a", "b", "c"})
	if !errors.Is(err, disk) {
		t.Fatalf("UseAll вернул %v, want ошибку Close", err)
	}
	if !strings.HasPrefix(err.Error(), "b: ") {
		t.Errorf("текст ошибки %q должен начинаться с %q", err, "b: ")
	}
	if slices.Contains(e.log, "open c") {
		t.Errorf("после ошибки обработка должна остановиться: %v", e.log)
	}
}

func TestUseAllUseErrorWinsOverClose(t *testing.T) {
	useErr := errors.New("use failed")
	closeErr := errors.New("close failed")
	e := &env{useErr: map[string]error{"a": useErr}, closeErr: map[string]error{"a": closeErr}}
	err := UseAll(e.opener, []string{"a"})
	if !errors.Is(err, useErr) {
		t.Fatalf("UseAll вернул %v, want ошибку Use", err)
	}
	if !slices.Contains(e.log, "close a") {
		t.Errorf("ресурс не закрыт после ошибки Use: %v", e.log)
	}
}

func TestUseAllOpenError(t *testing.T) {
	nope := errors.New("no such resource")
	e := &env{openErr: map[string]error{"b": nope}}
	err := UseAll(e.opener, []string{"a", "b", "c"})
	if !errors.Is(err, nope) || !strings.HasPrefix(err.Error(), "b: ") {
		t.Fatalf("UseAll вернул %v, want \"b: ...\" с ошибкой open", err)
	}
	want := []string{"open a", "use a", "close a", "open b"}
	if !slices.Equal(e.log, want) {
		t.Errorf("журнал = %v, want %v", e.log, want)
	}
}

func TestUseAllOneAtATime(t *testing.T) {
	e := &env{}
	names := []string{"a", "b", "c", "d", "e"}
	if err := UseAll(e.opener, names); err != nil {
		t.Fatalf("UseAll вернул %v", err)
	}
	if e.maxOpen != 1 || e.open != 0 {
		t.Errorf("maxOpen = %d, open = %d; want 1 и 0: закрывайте ресурс до открытия следующего", e.maxOpen, e.open)
	}
}

func TestUseAllEmpty(t *testing.T) {
	e := &env{}
	if err := UseAll(e.opener, nil); err != nil || len(e.log) != 0 {
		t.Errorf("UseAll(nil) = %v, журнал %v; want nil и пустой журнал", err, e.log)
	}
}

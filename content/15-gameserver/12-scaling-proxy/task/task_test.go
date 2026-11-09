package task

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

// fakeMover хранит состояние игроков по бэкендам и записывает вызовы.
type fakeMover struct {
	mu      sync.Mutex
	calls   []string
	saveErr error
	loadErr error
}

func (f *fakeMover) Save(backend, player string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "save "+backend+" "+player)
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	return []byte("state of " + player + " from " + backend), nil
}

func (f *fakeMover) Load(backend, player string, state []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("load %s %s [%s]", backend, player, state))
	return f.loadErr
}

func (f *fakeMover) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

var network = []Backend{
	{Name: "lobby-1", World: "lobby", Max: 2},
	{Name: "lobby-2", World: "lobby", Max: 2},
	{Name: "survival-1", World: "survival", Max: 3},
}

func TestConnectLeastLoaded(t *testing.T) {
	r := NewRouter(network, &fakeMover{})
	var got []string
	for i := range 4 {
		name, err := r.Connect(fmt.Sprint("p", i), "lobby")
		if err != nil {
			t.Fatalf("Connect p%d: %v", i, err)
		}
		got = append(got, name)
	}
	want := []string{"lobby-1", "lobby-2", "lobby-1", "lobby-2"}
	if !slices.Equal(got, want) {
		t.Fatalf("бэкенды = %v, want %v", got, want)
	}
	if _, err := r.Connect("p4", "lobby"); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("лобби заполнены: %v, want ErrNoBackend", err)
	}
	if _, err := r.Connect("p0", "survival"); !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("повторный Connect: %v, want ErrAlreadyConnected", err)
	}
	if where, ok := r.Where("p2"); !ok || where != "lobby-1" {
		t.Fatalf("Where(p2) = %q %v, want lobby-1 true", where, ok)
	}
}

func TestTransfer(t *testing.T) {
	m := &fakeMover{}
	r := NewRouter(network, m)
	r.Connect("steve", "lobby")
	to, err := r.Transfer("steve", "survival")
	if err != nil || to != "survival-1" {
		t.Fatalf("Transfer = %q, %v; want survival-1, nil", to, err)
	}
	want := []string{"save lobby-1 steve", "load survival-1 steve [state of steve from lobby-1]"}
	if got := m.log(); !slices.Equal(got, want) {
		t.Fatalf("вызовы Mover = %q, want %q", got, want)
	}
	if r.Load("lobby-1") != 0 || r.Load("survival-1") != 1 {
		t.Fatalf("Load: lobby-1 %d, survival-1 %d; want 0 и 1", r.Load("lobby-1"), r.Load("survival-1"))
	}
	if where, _ := r.Where("steve"); where != "survival-1" {
		t.Fatalf("Where = %q, want survival-1", where)
	}
}

func TestTransferSaveFails(t *testing.T) {
	boom := errors.New("бэкенд не отвечает")
	m := &fakeMover{saveErr: boom}
	r := NewRouter(network, m)
	r.Connect("steve", "lobby")
	if _, err := r.Transfer("steve", "survival"); !errors.Is(err, boom) {
		t.Fatalf("Transfer = %v, want ошибку Save", err)
	}
	if where, _ := r.Where("steve"); where != "lobby-1" {
		t.Fatalf("Where = %q, want lobby-1 — игрок остаётся на месте", where)
	}
	if r.Load("survival-1") != 0 || r.Load("lobby-1") != 1 {
		t.Fatalf("Load: survival-1 %d, lobby-1 %d; want 0 и 1 — резерв снят", r.Load("survival-1"), r.Load("lobby-1"))
	}
}

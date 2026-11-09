package task

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// blockingMover: Save для игрока из block ждёт release, остальные
// вызовы проходят сразу.
type blockingMover struct {
	fakeMover
	block   string
	entered chan struct{}
	release chan struct{}
}

func (b *blockingMover) Save(backend, player string) ([]byte, error) {
	if player == b.block {
		close(b.entered)
		<-b.release
	}
	return b.fakeMover.Save(backend, player)
}

func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s не вернулся, пока идёт перенос другого игрока: Mover вызывается под блокировкой?", what)
	}
}

func TestHiddenPickRules(t *testing.T) {
	bs := []Backend{
		{Name: "a", World: "lobby", Max: 1},
		{Name: "b", World: "lobby", Max: 2},
		{Name: "c", World: "lobby", Max: 3},
		{Name: "s", World: "survival", Max: 1},
	}
	r := NewRouter(bs, &fakeMover{})
	var got []string
	for i := range 7 {
		name, err := r.Connect(fmt.Sprint("p", i), "lobby")
		if err != nil {
			got = append(got, "ошибка")
			continue
		}
		got = append(got, name)
	}
	// a, b, c по одному → a полон; b и c поровну — b первый в списке;
	// b полон → c; все полны → ошибка.
	want := []string{"a", "b", "c", "b", "c", "c", "ошибка"}
	if !slices.Equal(got, want) {
		t.Fatalf("бэкенды = %v, want %v", got, want)
	}
	if _, err := r.Connect("x", "nether"); !errors.Is(err, ErrNoBackend) {
		t.Fatalf("неизвестный мир: %v, want ErrNoBackend", err)
	}
	// Нездоровый бэкенд не выбирается, но его игроки остаются.
	r.Disconnect("p1") // b: 1
	r.Disconnect("p2") // c: 2
	r.SetHealthy("b", false)
	if name, _ := r.Connect("q", "lobby"); name != "c" {
		t.Fatalf("b нездоров: Connect = %q, want c", name)
	}
	if where, _ := r.Where("p3"); where != "b" || r.Load("b") != 1 {
		t.Fatalf("игрок на нездоровом бэкенде: Where = %q, Load(b) = %d; want b, 1", where, r.Load("b"))
	}
	r.SetHealthy("b", true)
	if name, _ := r.Connect("q2", "lobby"); name != "b" {
		t.Fatalf("b снова здоров: Connect = %q, want b", name)
	}
	r.SetHealthy("nope", false) // неизвестное имя — без паники
	if r.Load("nope") != 0 {
		t.Fatal("Load неизвестного бэкенда не 0")
	}
}

func TestHiddenTransferSameWorldIsNoop(t *testing.T) {
	m := &fakeMover{}
	r := NewRouter(network, m)
	r.Connect("steve", "lobby")
	to, err := r.Transfer("steve", "lobby")
	if err != nil || to != "lobby-1" {
		t.Fatalf("Transfer в свой мир = %q, %v; want lobby-1, nil", to, err)
	}
	if calls := m.log(); len(calls) != 0 {
		t.Fatalf("Mover вызван: %q", calls)
	}
	if r.Load("lobby-1") != 1 || r.Load("lobby-2") != 0 {
		t.Fatal("Load изменился")
	}
}

func TestHiddenTransferLoadFails(t *testing.T) {
	boom := errors.New("нет места на диске")
	m := &fakeMover{loadErr: boom}
	r := NewRouter(network, m)
	r.Connect("steve", "lobby")
	if _, err := r.Transfer("steve", "survival"); !errors.Is(err, boom) {
		t.Fatalf("Transfer = %v, want ошибку Load", err)
	}
	if where, _ := r.Where("steve"); where != "lobby-1" {
		t.Fatalf("Where = %q, want lobby-1", where)
	}
	if r.Load("survival-1") != 0 || r.Load("lobby-1") != 1 {
		t.Fatalf("Load: survival-1 %d, lobby-1 %d; want 0 и 1", r.Load("survival-1"), r.Load("lobby-1"))
	}
	m.mu.Lock()
	m.loadErr = nil
	m.mu.Unlock()
	if to, err := r.Transfer("steve", "survival"); err != nil || to != "survival-1" {
		t.Fatalf("повторный Transfer = %q, %v", to, err)
	}
}

func TestHiddenSaveFailureSkipsLoad(t *testing.T) {
	m := &fakeMover{saveErr: errors.New("таймаут")}
	r := NewRouter(network, m)
	r.Connect("steve", "lobby")
	r.Transfer("steve", "survival")
	if calls := m.log(); len(calls) != 1 || calls[0] != "save lobby-1 steve" {
		t.Fatalf("вызовы = %q, want только save — Load без состояния не вызывается", calls)
	}
}

func TestHiddenErrors(t *testing.T) {
	r := NewRouter(network, &fakeMover{})
	if _, err := r.Transfer("ghost", "survival"); !errors.Is(err, ErrUnknownPlayer) {
		t.Errorf("Transfer неизвестного: %v", err)
	}
	if err := r.Disconnect("ghost"); !errors.Is(err, ErrUnknownPlayer) {
		t.Errorf("Disconnect неизвестного: %v", err)
	}
	if _, ok := r.Where("ghost"); ok {
		t.Error("Where неизвестного: ok = true")
	}
	r.Connect("steve", "survival")
	if _, err := r.Transfer("steve", "nether"); !errors.Is(err, ErrNoBackend) {
		t.Errorf("Transfer в мир без бэкендов: %v", err)
	}
	if where, _ := r.Where("steve"); where != "survival-1" {
		t.Errorf("после неудачного Transfer Where = %q", where)
	}
	if err := r.Disconnect("steve"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if err := r.Disconnect("steve"); !errors.Is(err, ErrUnknownPlayer) {
		t.Errorf("повторный Disconnect: %v", err)
	}
	if r.Load("survival-1") != 0 {
		t.Error("Load после выхода не 0")
	}
	if _, err := r.Connect("steve", "lobby"); err != nil {
		t.Errorf("Connect после выхода: %v", err)
	}
}

// Пока Save одного игрока висит в сети, роутер обслуживает других,
// а место на цели уже зарезервировано.
func TestHiddenNoLockDuringMove(t *testing.T) {
	m := &blockingMover{block: "steve", entered: make(chan struct{}), release: make(chan struct{})}
	bs := []Backend{
		{Name: "lobby-1", World: "lobby", Max: 10},
		{Name: "survival-1", World: "survival", Max: 1},
		{Name: "survival-2", World: "survival", Max: 1},
	}
	r := NewRouter(bs, m)
	r.Connect("steve", "lobby")
	r.Connect("alex", "lobby")
	done := make(chan error, 1)
	go func() {
		_, err := r.Transfer("steve", "survival")
		done <- err
	}()
	select {
	case <-m.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Save не вызван")
	}
	within(t, "Load/Where/Connect/Disconnect/Transfer", func() {
		if r.Load("survival-1") != 1 {
			t.Errorf("Load(survival-1) во время переноса = %d, want 1 — резерв", r.Load("survival-1"))
		}
		if where, _ := r.Where("steve"); where != "lobby-1" {
			t.Errorf("Where(steve) во время переноса = %q, want lobby-1", where)
		}
		if name, err := r.Connect("bob", "survival"); err != nil || name != "survival-2" {
			t.Errorf("Connect(bob) = %q, %v; want survival-2 — survival-1 занят резервом", name, err)
		}
		if _, err := r.Connect("eve", "survival"); !errors.Is(err, ErrNoBackend) {
			t.Errorf("Connect(eve) = %v, want ErrNoBackend", err)
		}
		if to, err := r.Transfer("alex", "survival"); !errors.Is(err, ErrNoBackend) {
			t.Errorf("Transfer(alex) = %q, %v; want ErrNoBackend", to, err)
		}
		r.Disconnect("bob")
	})
	close(m.release)
	if err := <-done; err != nil {
		t.Fatalf("Transfer(steve): %v", err)
	}
	if where, _ := r.Where("steve"); where != "survival-1" {
		t.Fatalf("Where(steve) = %q, want survival-1", where)
	}
	if r.Load("lobby-1") != 1 || r.Load("survival-1") != 1 || r.Load("survival-2") != 0 {
		t.Fatalf("Load = %d %d %d, want 1 1 0", r.Load("lobby-1"), r.Load("survival-1"), r.Load("survival-2"))
	}
}

// Стресс под -race: игроки ходят между мирами одновременно;
// вместимость не превышается, в конце все места свободны.
func TestHiddenConcurrent(t *testing.T) {
	bs := []Backend{
		{Name: "lobby-1", World: "lobby", Max: 40},
		{Name: "lobby-2", World: "lobby", Max: 40},
		{Name: "survival-1", World: "survival", Max: 30},
		{Name: "survival-2", World: "survival", Max: 30},
	}
	r := NewRouter(bs, &fakeMover{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	over := 0
	for i := range 60 {
		wg.Go(func() {
			p := fmt.Sprint("p", i)
			if _, err := r.Connect(p, "lobby"); err != nil {
				t.Errorf("Connect %s: %v", p, err)
				return
			}
			for range 20 {
				r.Transfer(p, "survival") // при нехватке мест — ErrNoBackend, это нормально
				for _, b := range bs {
					if r.Load(b.Name) > b.Max {
						mu.Lock()
						over++
						mu.Unlock()
					}
				}
				r.Transfer(p, "lobby")
			}
			if err := r.Disconnect(p); err != nil {
				t.Errorf("Disconnect %s: %v", p, err)
			}
		})
	}
	wg.Wait()
	if over > 0 {
		t.Errorf("вместимость превышена %d раз", over)
	}
	for _, b := range bs {
		if got := r.Load(b.Name); got != 0 {
			t.Errorf("Load(%s) = %d после выхода всех, want 0", b.Name, got)
		}
	}
}

// Max <= 0 — бэкенд не выбирается; уйти можно и с нездорового бэкенда.
func TestHiddenZeroMaxAndUnhealthySource(t *testing.T) {
	bs := []Backend{
		{Name: "dead", World: "lobby", Max: 0},
		{Name: "lobby-1", World: "lobby", Max: 5},
		{Name: "neg", World: "survival", Max: -1},
		{Name: "survival-1", World: "survival", Max: 5},
	}
	r := NewRouter(bs, &fakeMover{})
	if name, err := r.Connect("steve", "lobby"); err != nil || name != "lobby-1" {
		t.Fatalf("Connect = %q, %v; want lobby-1 — Max 0 не выбирается", name, err)
	}
	r.SetHealthy("lobby-1", false)
	if to, err := r.Transfer("steve", "survival"); err != nil || to != "survival-1" {
		t.Fatalf("Transfer с нездорового = %q, %v; want survival-1", to, err)
	}
	if r.Load("lobby-1") != 0 || r.Load("neg") != 0 || r.Load("dead") != 0 {
		t.Fatal("неверный Load после переноса")
	}
}

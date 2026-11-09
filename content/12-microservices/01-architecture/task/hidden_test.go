package task

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

// spyStock записывает вызовы и возвращает заданные ошибки.
type spyStock struct {
	mu         sync.Mutex
	reserves   [][]Line
	reserveIDs []string
	releases   []string
	reserveErr error
	releaseErr error
	ctxOK      []bool // был ли в ctx ключ ctxKey
}

type ctxKey struct{}

func (s *spyStock) Reserve(ctx context.Context, id string, lines []Line) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserves = append(s.reserves, slices.Clone(lines))
	s.reserveIDs = append(s.reserveIDs, id)
	s.ctxOK = append(s.ctxOK, ctx.Value(ctxKey{}) != nil)
	return s.reserveErr
}

func (s *spyStock) Release(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases = append(s.releases, id)
	return s.releaseErr
}

func TestInvalidMakesNoCalls(t *testing.T) {
	cases := []struct {
		name     string
		customer string
		lines    []Line
	}{
		{"пустой покупатель", "", []Line{{"tea", 1}}},
		{"nil позиции", "alice", nil},
		{"пустые позиции", "alice", []Line{}},
		{"qty 0", "alice", []Line{{"tea", 1}, {"cup", 0}}},
		{"qty < 0", "alice", []Line{{"tea", -2}}},
		{"пустой SKU", "alice", []Line{{"", 1}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stock := &spyStock{}
			pub := &recPublisher{}
			ids := &seqIDs{}
			svc := NewService(stock, pub, ids.next)
			_, err := svc.PlaceOrder(context.Background(), c.customer, c.lines)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if len(stock.reserves) != 0 || len(stock.releases) != 0 || len(pub.list()) != 0 || ids.calls() != 0 {
				t.Errorf("невалидный заказ дошёл до зависимостей: reserve=%d release=%d publish=%d newID=%d",
					len(stock.reserves), len(stock.releases), len(pub.list()), ids.calls())
			}
		})
	}
}

func TestMergeAndSortLines(t *testing.T) {
	stock := &spyStock{}
	svc := NewService(stock, &recPublisher{}, (&seqIDs{}).next)
	o, err := svc.PlaceOrder(context.Background(), "alice", []Line{{"tea", 1}, {"cup", 2}, {"tea", 3}, {"bag", 1}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Line{{"bag", 1}, {"cup", 2}, {"tea", 4}}
	if len(stock.reserves) != 1 || !slices.Equal(stock.reserves[0], want) {
		t.Errorf("Reserve получил %v, want ровно один вызов с %v", stock.reserves, want)
	}
	if !slices.Equal(o.Lines, want) {
		t.Errorf("Order.Lines = %v, want %v", o.Lines, want)
	}
	if len(stock.reserveIDs) != 1 || stock.reserveIDs[0] != o.ID {
		t.Errorf("Reserve получил id %v, а заказ %q", stock.reserveIDs, o.ID)
	}
}

func TestNewIDOncePerOrder(t *testing.T) {
	ids := &seqIDs{}
	stock := NewMemStock(map[string]int{"tea": 1})
	svc := NewService(stock, &recPublisher{}, ids.next)
	ctx := context.Background()
	svc.PlaceOrder(ctx, "a", []Line{{"tea", 1}})
	svc.PlaceOrder(ctx, "b", []Line{{"tea", 1}}) // не хватит товара
	o, err := svc.PlaceOrder(ctx, "c", []Line{{"tea", 1}, {"tea", 0}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if o.ID != "" {
		t.Errorf("при ошибке PlaceOrder вернул заказ %+v, want нулевой", o)
	}
	if ids.calls() != 2 {
		t.Errorf("newID вызван %d раз, want 2 (по разу на каждый валидный заказ)", ids.calls())
	}
}

func TestContextPassedToDependencies(t *testing.T) {
	stock := &spyStock{}
	var pubCtx atomic.Bool
	pub := publisherFunc(func(ctx context.Context, e Event) error {
		pubCtx.Store(ctx.Value(ctxKey{}) != nil)
		return nil
	})
	svc := NewService(stock, pub, (&seqIDs{}).next)
	ctx := context.WithValue(context.Background(), ctxKey{}, 1)
	if _, err := svc.PlaceOrder(ctx, "alice", []Line{{"tea", 1}}); err != nil {
		t.Fatal(err)
	}
	if len(stock.ctxOK) != 1 || !stock.ctxOK[0] || !pubCtx.Load() {
		t.Error("контекст запроса не дошёл до Reserve или Publish")
	}
}

type publisherFunc func(ctx context.Context, e Event) error

func (f publisherFunc) Publish(ctx context.Context, e Event) error { return f(ctx, e) }

func TestReserveErrorWrapped(t *testing.T) {
	errDown := errors.New("warehouse unavailable")
	stock := &spyStock{reserveErr: errDown}
	pub := &recPublisher{}
	svc := NewService(stock, pub, (&seqIDs{}).next)
	_, err := svc.PlaceOrder(context.Background(), "alice", []Line{{"tea", 1}})
	if !errors.Is(err, errDown) {
		t.Fatalf("err = %v, want обёртку над ошибкой склада", err)
	}
	if len(pub.list()) != 0 || len(stock.releases) != 0 {
		t.Errorf("после отказа Reserve: publish=%d release=%d, want 0 и 0", len(pub.list()), len(stock.releases))
	}
	if _, ok := svc.Get("o-1"); ok {
		t.Error("заказ сохранён после отказа склада")
	}
}

func TestPublishFailureReleases(t *testing.T) {
	errBroker := errors.New("broker down")
	stock := NewMemStock(map[string]int{"tea": 5})
	svc := NewService(stock, &recPublisher{fail: errBroker}, (&seqIDs{}).next)
	_, err := svc.PlaceOrder(context.Background(), "alice", []Line{{"tea", 2}})
	if !errors.Is(err, errBroker) {
		t.Fatalf("err = %v, want обёртку над ошибкой публикации", err)
	}
	if got := stock.Available("tea"); got != 5 {
		t.Errorf("резерв не снят: Available(tea) = %d, want 5", got)
	}
	if _, ok := svc.Get("o-1"); ok {
		t.Error("заказ сохранён, хотя событие не опубликовано")
	}

	spy := &spyStock{}
	svc = NewService(spy, &recPublisher{fail: errBroker}, (&seqIDs{}).next)
	svc.PlaceOrder(context.Background(), "alice", []Line{{"tea", 2}})
	if !slices.Equal(spy.releases, []string{"o-1"}) {
		t.Errorf("Release вызван с %v, want [o-1]", spy.releases)
	}
}

func TestReleaseFailureJoined(t *testing.T) {
	errBroker := errors.New("broker down")
	errRelease := errors.New("release failed")
	spy := &spyStock{releaseErr: errRelease}
	svc := NewService(spy, &recPublisher{fail: errBroker}, (&seqIDs{}).next)
	_, err := svc.PlaceOrder(context.Background(), "alice", []Line{{"tea", 1}})
	if !errors.Is(err, errBroker) || !errors.Is(err, errRelease) {
		t.Fatalf("err = %v, want и ошибку публикации, и ошибку Release", err)
	}
}

func TestCompensationSurvivesCancel(t *testing.T) {
	stock := NewMemStock(map[string]int{"tea": 5})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Клиент ушёл, пока публиковали событие: ctx отменён.
	pub := publisherFunc(func(ctx context.Context, e Event) error {
		cancel()
		return ctx.Err()
	})
	svc := NewService(stock, pub, (&seqIDs{}).next)
	_, err := svc.PlaceOrder(ctx, "alice", []Line{{"tea", 2}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := stock.Available("tea"); got != 5 {
		t.Errorf("резерв не снят после отмены запроса: Available(tea) = %d, want 5", got)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	svc := NewService(NewMemStock(map[string]int{"tea": 5, "cup": 5}), &recPublisher{}, (&seqIDs{}).next)
	lines := []Line{{"tea", 1}, {"cup", 1}}
	o, err := svc.PlaceOrder(context.Background(), "alice", lines)
	if err != nil || len(o.Lines) != 2 {
		t.Fatalf("PlaceOrder = %+v, %v", o, err)
	}
	o.Lines[0].Qty = 100
	lines[0].Qty = 100
	g, ok := svc.Get("o-1")
	if !ok || len(g.Lines) != 2 {
		t.Fatalf("Get(o-1) = %+v, %v", g, ok)
	}
	g.Lines[1].Qty = 100
	g2, ok := svc.Get("o-1")
	if !ok || !slices.Equal(g2.Lines, []Line{{"cup", 1}, {"tea", 1}}) {
		t.Errorf("сохранённый заказ изменился через чужой срез: %+v", g2)
	}
	if _, ok := svc.Get("o-404"); ok {
		t.Error("Get(o-404) нашёл заказ")
	}
}

func TestMemStockAllOrNothing(t *testing.T) {
	ctx := context.Background()
	m := NewMemStock(map[string]int{"tea": 3, "cup": 1})
	err := m.Reserve(ctx, "a", []Line{{"tea", 2}, {"cup", 2}})
	if !errors.Is(err, ErrOutOfStock) {
		t.Fatalf("err = %v, want ErrOutOfStock", err)
	}
	if m.Available("tea") != 3 || m.Available("cup") != 1 {
		t.Errorf("частичный резерв: tea=%d cup=%d", m.Available("tea"), m.Available("cup"))
	}
	// Одинаковые SKU складываются: 2 + 2 > 3.
	if err := m.Reserve(ctx, "b", []Line{{"tea", 2}, {"tea", 2}}); !errors.Is(err, ErrOutOfStock) {
		t.Errorf("Reserve(tea 2 + tea 2) при остатке 3: err = %v, want ErrOutOfStock", err)
	}
	if err := m.Reserve(ctx, "c", []Line{{"ghost", 1}}); !errors.Is(err, ErrOutOfStock) {
		t.Errorf("неизвестный SKU: err = %v, want ErrOutOfStock", err)
	}
	if err := m.Reserve(ctx, "d", []Line{{"tea", 0}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("qty 0: err = %v, want ErrInvalid", err)
	}
	if m.Available("tea") != 3 {
		t.Errorf("Available(tea) = %d, want 3", m.Available("tea"))
	}
}

func TestMemStockIdempotent(t *testing.T) {
	ctx := context.Background()
	m := NewMemStock(map[string]int{"tea": 5})
	for range 2 {
		if err := m.Reserve(ctx, "a", []Line{{"tea", 2}}); err != nil {
			t.Fatalf("Reserve(a): %v", err)
		}
	}
	if got := m.Available("tea"); got != 3 {
		t.Errorf("повторный Reserve того же заказа списал ещё раз: Available = %d, want 3", got)
	}
	for range 2 {
		if err := m.Release(ctx, "a"); err != nil {
			t.Fatalf("Release(a): %v", err)
		}
	}
	if got := m.Available("tea"); got != 5 {
		t.Errorf("повторный Release вернул лишнее: Available = %d, want 5", got)
	}
	if err := m.Release(ctx, "never"); err != nil {
		t.Errorf("Release неизвестного заказа: %v, want nil", err)
	}
	// После Release тот же id можно зарезервировать снова.
	if err := m.Reserve(ctx, "a", []Line{{"tea", 4}}); err != nil || m.Available("tea") != 1 {
		t.Errorf("Reserve после Release: err=%v Available=%d, want nil и 1", err, m.Available("tea"))
	}
}

func TestMemStockCanceled(t *testing.T) {
	m := NewMemStock(map[string]int{"tea": 5})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Reserve(ctx, "a", []Line{{"tea", 1}}); !errors.Is(err, context.Canceled) {
		t.Errorf("Reserve с отменённым ctx: err = %v, want context.Canceled", err)
	}
	if m.Available("tea") != 5 {
		t.Errorf("Reserve с отменённым ctx списал товар")
	}
	if err := m.Release(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Errorf("Release с отменённым ctx: err = %v, want context.Canceled", err)
	}
}

func TestMemStockCopiesInput(t *testing.T) {
	initial := map[string]int{"tea": 5}
	m := NewMemStock(initial)
	initial["tea"] = 100
	if got := m.Available("tea"); got != 5 {
		t.Errorf("изменение исходной map видно складу: Available = %d, want 5", got)
	}
	m.Reserve(context.Background(), "a", []Line{{"tea", 1}})
	if initial["tea"] != 100 {
		t.Errorf("склад изменил переданную map")
	}
}

func TestConcurrentOrders(t *testing.T) {
	stock := NewMemStock(map[string]int{"tea": 50})
	pub := &recPublisher{}
	svc := NewService(stock, pub, (&seqIDs{}).next)
	var ok, out atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.PlaceOrder(context.Background(), "c", []Line{{"tea", 1}})
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrOutOfStock):
				out.Add(1)
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 50 || out.Load() != 50 || stock.Available("tea") != 0 || len(pub.list()) != 50 {
		t.Errorf("успешных %d, отказов %d, остаток %d, событий %d; want 50, 50, 0, 50",
			ok.Load(), out.Load(), stock.Available("tea"), len(pub.list()))
	}
}

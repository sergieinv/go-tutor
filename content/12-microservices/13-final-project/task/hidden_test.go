package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ctxKey struct{}

// reqCtx — контекст запроса с меткой: по ней видно, что
// производный контекст сохранил значения.
func reqCtx() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "req-1"))
}

func item(id int64, q int) Item { return Item{ProductID: id, Quantity: q} }

func TestHiddenValidation(t *testing.T) {
	many := make([]Item, MaxItems+1)
	for i := range many {
		many[i] = item(int64(i+1), 1)
	}
	cases := []struct {
		name     string
		customer int64
		items    []Item
	}{
		{"нулевой покупатель", 0, []Item{item(1, 1)}},
		{"отрицательный покупатель", -1, []Item{item(1, 1)}},
		{"nil позиции", 1, nil},
		{"пустые позиции", 1, []Item{}},
		{"больше MaxItems", 1, many},
		{"нулевой товар", 1, []Item{item(0, 1)}},
		{"отрицательный товар", 1, []Item{item(-2, 1)}},
		{"количество 0", 1, []Item{item(1, 0)}},
		{"количество больше MaxQuantity", 1, []Item{item(1, MaxQuantity+1)}},
		{"повтор товара", 1, []Item{item(1, 1), item(2, 1), item(1, 3)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, wh := &fakeStore{}, &fakeWarehouse{}
			s := &Saga{Store: st, Warehouse: wh}
			o, err := s.CreateOrder(context.Background(), c.customer, c.items)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if o.ID != 0 || o.CustomerID != 0 || o.Items != nil {
				t.Fatalf("заказ = %+v, want нулевой", o)
			}
			if m := append(st.methods(), wh.methods()...); len(m) != 0 {
				t.Fatalf("при ошибке валидации вызваны %v", m)
			}
		})
	}
}

func TestHiddenBoundaries(t *testing.T) {
	items := make([]Item, MaxItems)
	for i := range items {
		items[i] = item(int64(MaxItems-i), MaxQuantity) // по убыванию
	}
	orig := slices.Clone(items)
	st, wh := &fakeStore{}, &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	o, err := s.CreateOrder(context.Background(), 1, items)
	if err != nil {
		t.Fatalf("100 позиций по 1000 — допустимо, got %v", err)
	}
	if !slices.Equal(items, orig) {
		t.Fatal("CreateOrder изменил переданный срез items")
	}
	if !slices.IsSortedFunc(o.Items, func(a, b Item) int { return int(a.ProductID - b.ProductID) }) || len(o.Items) != MaxItems {
		t.Fatalf("Items заказа не отсортированы по ProductID или потеряны: %d позиций", len(o.Items))
	}
	if len(wh.items) != 1 || !slices.Equal(wh.items[0], o.Items) {
		t.Fatal("Reserve должен получить те же позиции, что в заказе")
	}
	if len(st.saved) != 1 || !slices.Equal(st.saved[0].Items, o.Items) || st.saved[0].ID != o.ID || st.saved[0].CustomerID != 1 {
		t.Fatalf("Save получил %+v, want тот же заказ, что вернулся", st.saved)
	}
}

func TestHiddenContextsPassedThrough(t *testing.T) {
	ctx, cancel := reqCtx()
	defer cancel()
	st, wh := &fakeStore{}, &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	if _, err := s.CreateOrder(ctx, 5, []Item{item(1, 1)}); err != nil {
		t.Fatal(err)
	}
	for _, c := range append(st.find("NextID"), append(st.find("Save"), wh.find("Reserve")...)...) {
		if c.ctx.Value(ctxKey{}) != "req-1" {
			t.Fatalf("%s получил контекст без значений запроса", c.method)
		}
	}
	if r, sv := wh.find("Reserve"), st.find("Save"); len(r) != 1 || len(sv) != 1 || r[0].id != 1 || sv[0].id != 1 {
		t.Fatal("номер из NextID должен уйти в Reserve и Save")
	}
}

func TestHiddenNextIDError(t *testing.T) {
	boom := errors.New("sequence broken")
	st, wh := &fakeStore{nextErr: boom}, &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	o, err := s.CreateOrder(context.Background(), 5, []Item{item(1, 1)})
	if !errors.Is(err, boom) || o.ID != 0 {
		t.Fatalf("CreateOrder = %+v, %v; want нулевой заказ и ошибку NextID", o, err)
	}
	if m := wh.methods(); len(m) != 0 {
		t.Fatalf("после ошибки NextID склад вызывать нельзя: %v", m)
	}
	if m := st.methods(); !slices.Equal(m, []string{"NextID"}) {
		t.Fatalf("вызовы хранилища = %v, want [NextID]", m)
	}
}

// Отказы склада, после которых резерва точно нет.
func TestHiddenDefiniteRejections(t *testing.T) {
	otherFP, _ := status.New(codes.FailedPrecondition, "frozen").WithDetails(
		&errdetails.ErrorInfo{Reason: "PRODUCT_FROZEN", Domain: "warehouse.example"})
	cases := []struct {
		name       string
		err        error
		outOfStock bool
	}{
		{"OUT_OF_STOCK", outOfStockErr(), true},
		{"OUT_OF_STOCK обёрнут", fmt.Errorf("warehouse client: %w", outOfStockErr()), true},
		{"FailedPrecondition без ErrorInfo", status.Error(codes.FailedPrecondition, "no"), false},
		{"FailedPrecondition с другой причиной", otherFP.Err(), false},
		{"InvalidArgument", status.Error(codes.InvalidArgument, "bad items"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &fakeStore{}
			wh := &fakeWarehouse{reserve: func(context.Context, int64) error { return c.err }}
			s := &Saga{Store: st, Warehouse: wh}
			o, err := s.CreateOrder(context.Background(), 5, []Item{item(1, 1)})
			if err == nil || o.ID != 0 {
				t.Fatalf("CreateOrder = %+v, %v; want ошибку и нулевой заказ", o, err)
			}
			if !errors.Is(err, c.err) {
				t.Fatalf("err = %v: errors.Is не находит ошибку Reserve", err)
			}
			if errors.Is(err, ErrOutOfStock) != c.outOfStock {
				t.Fatalf("errors.Is(err, ErrOutOfStock) = %v, want %v", !c.outOfStock, c.outOfStock)
			}
			if errors.Is(err, ErrUnavailable) {
				t.Fatal("ответ склада — не ErrUnavailable")
			}
			if m := wh.methods(); !slices.Equal(m, []string{"Reserve"}) {
				t.Fatalf("вызовы склада = %v: компенсировать нечего", m)
			}
			if m := st.methods(); !slices.Equal(m, []string{"NextID"}) {
				t.Fatalf("вызовы хранилища = %v, want [NextID]", m)
			}
		})
	}
}

// Исход неизвестен — нужна компенсация.
func TestHiddenUnknownOutcome(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		unavailable bool
	}{
		{"Unavailable", status.Error(codes.Unavailable, "down"), true},
		{"Unavailable обёрнут", fmt.Errorf("retry: %w", status.Error(codes.Unavailable, "down")), true},
		{"DeadlineExceeded", status.Error(codes.DeadlineExceeded, "slow"), true},
		{"Internal", status.Error(codes.Internal, "internal error"), false},
		{"Aborted", status.Error(codes.Aborted, "released"), false},
		{"не gRPC", errors.New("connection reset"), false},
		{"context.Canceled", context.Canceled, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &fakeStore{}
			wh := &fakeWarehouse{reserve: func(context.Context, int64) error { return c.err }}
			s := &Saga{Store: st, Warehouse: wh}
			o, err := s.CreateOrder(context.Background(), 5, []Item{item(1, 1)})
			if !errors.Is(err, c.err) || o.ID != 0 {
				t.Fatalf("CreateOrder = %+v, %v; want нулевой заказ и ошибку Reserve", o, err)
			}
			if errors.Is(err, ErrUnavailable) != c.unavailable {
				t.Fatalf("errors.Is(err, ErrUnavailable) = %v, want %v", !c.unavailable, c.unavailable)
			}
			if errors.Is(err, ErrOutOfStock) {
				t.Fatal("это не ErrOutOfStock")
			}
			rel := wh.find("Release")
			if len(rel) != 1 || rel[0].id != 1 {
				t.Fatalf("Release = %+v, want ровно один для заказа 1", rel)
			}
			if m := st.methods(); !slices.Equal(m, []string{"NextID"}) {
				t.Fatalf("вызовы хранилища = %v: Release удался, в очередь ставить нечего", m)
			}
		})
	}
}

func checkDetached(t *testing.T, what string, c context.Context, timeout time.Duration, start time.Time) {
	t.Helper()
	if c.Err() != nil {
		t.Fatalf("%s: контекст уже отменён (%v) — компенсация должна пережить запрос", what, c.Err())
	}
	if c.Value(ctxKey{}) != "req-1" {
		t.Fatalf("%s: контекст потерял значения запроса", what)
	}
	dl, ok := c.Deadline()
	if !ok {
		t.Fatalf("%s: у контекста нет дедлайна — нужен ReleaseTimeout", what)
	}
	if dl.Before(start.Add(timeout/2)) || dl.After(time.Now().Add(timeout)) {
		t.Fatalf("%s: дедлайн через %v, want около %v", what, dl.Sub(start), timeout)
	}
}

func TestHiddenReleaseContextDetached(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     time.Duration
		timeout time.Duration
	}{
		{"по умолчанию", 0, DefaultReleaseTimeout},
		{"свой таймаут", time.Hour, time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := reqCtx()
			defer cancel()
			st := &fakeStore{}
			// Клиент ушёл, пока шёл Reserve.
			wh := &fakeWarehouse{reserve: func(context.Context, int64) error {
				cancel()
				return status.Error(codes.Canceled, "context canceled")
			}}
			var seen context.Context
			wh.release = func(c context.Context, _ int64) error {
				seen = c
				checkDetached(t, "Release", c, tc.timeout, time.Now().Add(-time.Second))
				return nil
			}
			s := &Saga{Store: st, Warehouse: wh, ReleaseTimeout: tc.cfg}
			if _, err := s.CreateOrder(ctx, 5, []Item{item(1, 1)}); err == nil {
				t.Fatal("want ошибку")
			}
			if seen == nil {
				t.Fatal("Release не вызван")
			}
		})
	}
}

func TestHiddenReleaseFailsQueued(t *testing.T) {
	ctx, cancel := reqCtx()
	defer cancel()
	down := status.Error(codes.Unavailable, "down")
	relErr := status.Error(codes.Unavailable, "still down")
	st := &fakeStore{}
	wh := &fakeWarehouse{
		reserve: func(context.Context, int64) error { cancel(); return down },
		release: func(context.Context, int64) error { return relErr },
	}
	start := time.Now()
	st.hook = func(method string, c context.Context) {
		if method == "AddPendingRelease" {
			checkDetached(t, method, c, time.Minute, start)
		}
	}
	s := &Saga{Store: st, Warehouse: wh, ReleaseTimeout: time.Minute}
	_, err := s.CreateOrder(ctx, 5, []Item{item(1, 1)})
	if !errors.Is(err, down) || !errors.Is(err, relErr) || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ошибки Reserve и Release", err)
	}
	add := st.find("AddPendingRelease")
	if len(add) != 1 || add[0].id != 1 {
		t.Fatalf("AddPendingRelease = %+v, want один для заказа 1", add)
	}
	if !slices.Equal(st.pending, []int64{1}) {
		t.Fatalf("очередь = %v, want [1]", st.pending)
	}
}

func TestHiddenQueueFails(t *testing.T) {
	relErr := errors.New("release: connection refused")
	addErr := errors.New("queue: disk full")
	st := &fakeStore{save: func(Order) error { return errors.New("write failed") }, addErr: addErr}
	wh := &fakeWarehouse{release: func(context.Context, int64) error { return relErr }}
	s := &Saga{Store: st, Warehouse: wh}
	_, err := s.CreateOrder(context.Background(), 5, []Item{item(1, 1)})
	if !errors.Is(err, relErr) || !errors.Is(err, addErr) {
		t.Fatalf("err = %v, want ошибки Release и AddPendingRelease", err)
	}
}

func TestHiddenSaveFails(t *testing.T) {
	ctx, cancel := reqCtx()
	defer cancel()
	boom := errors.New("unique violation")
	st := &fakeStore{save: func(Order) error { cancel(); return boom }}
	wh := &fakeWarehouse{}
	wh.release = func(c context.Context, _ int64) error {
		checkDetached(t, "Release", c, DefaultReleaseTimeout, time.Now().Add(-time.Second))
		return nil
	}
	s := &Saga{Store: st, Warehouse: wh}
	o, err := s.CreateOrder(ctx, 5, []Item{item(1, 1)})
	if !errors.Is(err, boom) || o.ID != 0 {
		t.Fatalf("CreateOrder = %+v, %v", o, err)
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrOutOfStock) || errors.Is(err, ErrCommitUnknown) {
		t.Fatalf("err = %v: лишние доменные ошибки", err)
	}
	if rel := wh.find("Release"); len(rel) != 1 || rel[0].id != 1 {
		t.Fatalf("Release = %+v", rel)
	}
	if m := st.methods(); !slices.Equal(m, []string{"NextID", "Save"}) {
		t.Fatalf("вызовы хранилища = %v: Exists — только при ErrCommitUnknown", m)
	}
}

func commitUnknown() error { return fmt.Errorf("commit: %w: connection reset", ErrCommitUnknown) }

func TestHiddenCommitUnknownSaved(t *testing.T) {
	ctx, cancel := reqCtx()
	defer cancel()
	st := &fakeStore{}
	// COMMIT прошёл, но ответ потерян: заказ в базе есть.
	st.save = func(o Order) error { cancel(); st.saved = append(st.saved, o); return commitUnknown() }
	wh := &fakeWarehouse{}
	start := time.Now()
	st.hook = func(method string, c context.Context) {
		if method == "Exists" {
			checkDetached(t, method, c, time.Minute, start)
		}
	}
	s := &Saga{Store: st, Warehouse: wh, ReleaseTimeout: time.Minute}
	o, err := s.CreateOrder(ctx, 5, []Item{item(2, 1), item(1, 1)})
	if err != nil {
		t.Fatalf("заказ записан — CreateOrder должен вернуть успех, got %v", err)
	}
	if o.ID != 1 || o.CustomerID != 5 || !slices.Equal(o.Items, []Item{item(1, 1), item(2, 1)}) {
		t.Fatalf("заказ = %+v", o)
	}
	if m := wh.methods(); !slices.Equal(m, []string{"Reserve"}) {
		t.Fatalf("вызовы склада = %v: записанный заказ компенсировать нельзя", m)
	}
	ex := st.find("Exists")
	if len(ex) != 1 || ex[0].id != 1 {
		t.Fatalf("Exists = %+v, want один для заказа 1", ex)
	}
}

func TestHiddenCommitUnknownNotSaved(t *testing.T) {
	st := &fakeStore{save: func(Order) error { return commitUnknown() }}
	wh := &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	o, err := s.CreateOrder(context.Background(), 5, []Item{item(1, 1)})
	if !errors.Is(err, ErrCommitUnknown) || o.ID != 0 {
		t.Fatalf("CreateOrder = %+v, %v; want ErrCommitUnknown", o, err)
	}
	if m := wh.methods(); !slices.Equal(m, []string{"Reserve", "Release"}) {
		t.Fatalf("вызовы склада = %v: заказа нет — резерв вернуть", m)
	}
}

func TestHiddenCommitUnknownCheckFails(t *testing.T) {
	checkErr := errors.New("db unreachable")
	st := &fakeStore{
		save:   func(Order) error { return commitUnknown() },
		exists: func(int64) (bool, error) { return false, checkErr },
	}
	wh := &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	o, err := s.CreateOrder(context.Background(), 5, []Item{item(1, 1)})
	if !errors.Is(err, ErrCommitUnknown) || !errors.Is(err, checkErr) || o.ID != 0 {
		t.Fatalf("CreateOrder = %+v, %v; want ErrCommitUnknown и ошибку Exists", o, err)
	}
	if m := wh.methods(); !slices.Equal(m, []string{"Reserve"}) {
		t.Fatalf("вызовы склада = %v: исход неизвестен — резерв не трогаем", m)
	}
	if len(st.find("AddPendingRelease")) != 0 {
		t.Fatal("исход неизвестен — в очередь компенсаций не ставим")
	}
}

func TestHiddenRetryReleases(t *testing.T) {
	ctx, cancel := reqCtx()
	defer cancel()
	fail := status.Error(codes.Unavailable, "down")
	st := &fakeStore{pending: []int64{5, 2, 9}}
	wh := &fakeWarehouse{release: func(c context.Context, id int64) error {
		if c.Value(ctxKey{}) != "req-1" {
			t.Errorf("Release(%d): контекст не производный от ctx", id)
		}
		if dl, ok := c.Deadline(); !ok || dl.After(time.Now().Add(time.Minute)) {
			t.Errorf("Release(%d): нужен дедлайн не позже ReleaseTimeout", id)
		}
		if id == 2 {
			return fail
		}
		return nil
	}}
	s := &Saga{Store: st, Warehouse: wh, ReleaseTimeout: time.Minute}
	n, err := s.RetryReleases(ctx)
	if n != 2 || !errors.Is(err, fail) {
		t.Fatalf("RetryReleases = %d, %v; want 2 и ошибку Release заказа 2", n, err)
	}
	var ids []int64
	for _, c := range wh.find("Release") {
		ids = append(ids, c.id)
	}
	if !slices.Equal(ids, []int64{5, 2, 9}) {
		t.Fatalf("порядок Release = %v, want [5 2 9]: неудача не останавливает остальные", ids)
	}
	if !slices.Equal(st.pending, []int64{2}) {
		t.Fatalf("очередь = %v, want [2]", st.pending)
	}
	// Повтор, когда склад поднялся.
	wh.release = nil
	if n, err := s.RetryReleases(ctx); n != 1 || err != nil || len(st.pending) != 0 {
		t.Fatalf("второй проход = %d, %v, очередь %v; want 1, nil, пусто", n, err, st.pending)
	}
	if n, err := s.RetryReleases(ctx); n != 0 || err != nil {
		t.Fatalf("пустая очередь = %d, %v; want 0, nil", n, err)
	}
}

func TestHiddenRetryReleasesErrors(t *testing.T) {
	t.Run("ошибка списка", func(t *testing.T) {
		boom := errors.New("list failed")
		st, wh := &fakeStore{pending: []int64{1}, listErr: boom}, &fakeWarehouse{}
		n, err := (&Saga{Store: st, Warehouse: wh}).RetryReleases(context.Background())
		if n != 0 || !errors.Is(err, boom) || len(wh.methods()) != 0 {
			t.Fatalf("= %d, %v, склад %v", n, err, wh.methods())
		}
	})
	t.Run("ошибка удаления из очереди", func(t *testing.T) {
		boom := errors.New("remove failed")
		st := &fakeStore{pending: []int64{1, 2}, removeErr: func(id int64) error {
			if id == 1 {
				return boom
			}
			return nil
		}}
		wh := &fakeWarehouse{}
		n, err := (&Saga{Store: st, Warehouse: wh}).RetryReleases(context.Background())
		if n != 1 || !errors.Is(err, boom) {
			t.Fatalf("= %d, %v; want 1 и ошибку удаления", n, err)
		}
	})
	t.Run("ctx отменён заранее", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		st, wh := &fakeStore{pending: []int64{1, 2}}, &fakeWarehouse{}
		n, err := (&Saga{Store: st, Warehouse: wh}).RetryReleases(ctx)
		if n != 0 || !errors.Is(err, context.Canceled) || len(wh.methods()) != 0 {
			t.Fatalf("= %d, %v, склад %v; want 0, context.Canceled, без вызовов", n, err, wh.methods())
		}
	})
	t.Run("ctx отменён посреди прохода", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		st := &fakeStore{pending: []int64{1, 2, 3}}
		wh := &fakeWarehouse{release: func(context.Context, int64) error { cancel(); return nil }}
		n, err := (&Saga{Store: st, Warehouse: wh}).RetryReleases(ctx)
		if n != 1 || !errors.Is(err, context.Canceled) || len(wh.find("Release")) != 1 {
			t.Fatalf("= %d, %v, Release %d раз; want 1, context.Canceled, 1", n, err, len(wh.find("Release")))
		}
		if !slices.Equal(st.pending, []int64{2, 3}) {
			t.Fatalf("очередь = %v, want [2 3]", st.pending)
		}
	})
}

func TestHiddenConcurrent(t *testing.T) {
	st, wh := &fakeStore{}, &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			if _, err := s.CreateOrder(context.Background(), int64(i+1), []Item{item(1, 1), item(2, 2)}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(st.saved) != 16 {
		t.Fatalf("записано %d заказов, want 16", len(st.saved))
	}
}

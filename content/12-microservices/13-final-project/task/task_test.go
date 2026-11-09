package task

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// call — один вызов фейка: метод, номер заказа и контекст.
type call struct {
	method string
	id     int64
	ctx    context.Context
}

// fakeWarehouse — «Склад», ответы которого задаёт тест.
type fakeWarehouse struct {
	mu      sync.Mutex
	calls   []call
	items   [][]Item
	reserve func(ctx context.Context, id int64) error // nil — успех
	release func(ctx context.Context, id int64) error // nil — успех
}

func (w *fakeWarehouse) Reserve(ctx context.Context, id int64, items []Item) error {
	w.mu.Lock()
	w.calls = append(w.calls, call{"Reserve", id, ctx})
	w.items = append(w.items, slices.Clone(items))
	f := w.reserve
	w.mu.Unlock()
	if f == nil {
		return nil
	}
	return f(ctx, id)
}

func (w *fakeWarehouse) Release(ctx context.Context, id int64) error {
	w.mu.Lock()
	w.calls = append(w.calls, call{"Release", id, ctx})
	f := w.release
	w.mu.Unlock()
	if f == nil {
		return nil
	}
	return f(ctx, id)
}

func (w *fakeWarehouse) methods() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, c := range w.calls {
		out = append(out, c.method)
	}
	return out
}

func (w *fakeWarehouse) find(method string) []call {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []call
	for _, c := range w.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// fakeStore — хранилище «Заказов» в памяти с внедряемыми сбоями.
type fakeStore struct {
	mu      sync.Mutex
	next    int64
	saved   []Order
	pending []int64
	calls   []call

	nextErr   error
	save      func(o Order) error          // nil — успех
	exists    func(id int64) (bool, error) // nil — есть ли в saved
	addErr    error
	listErr   error
	removeErr func(id int64) error
	// hook видит каждый вызов в момент вызова (контекст ещё «живой»).
	hook func(method string, ctx context.Context)
}

func (s *fakeStore) log(method string, id int64, ctx context.Context) {
	s.calls = append(s.calls, call{method, id, ctx})
	if s.hook != nil {
		s.hook(method, ctx)
	}
}

func (s *fakeStore) NextID(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("NextID", 0, ctx)
	if s.nextErr != nil {
		return 0, s.nextErr
	}
	s.next++
	return s.next, nil
}

func (s *fakeStore) Save(ctx context.Context, o Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("Save", o.ID, ctx)
	o.Items = slices.Clone(o.Items)
	if s.save != nil {
		if err := s.save(o); err != nil {
			return err
		}
	}
	s.saved = append(s.saved, o)
	return nil
}

func (s *fakeStore) Exists(ctx context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("Exists", id, ctx)
	if s.exists != nil {
		return s.exists(id)
	}
	return slices.ContainsFunc(s.saved, func(o Order) bool { return o.ID == id }), nil
}

func (s *fakeStore) AddPendingRelease(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("AddPendingRelease", id, ctx)
	if s.addErr != nil {
		return s.addErr
	}
	s.pending = append(s.pending, id)
	return nil
}

func (s *fakeStore) PendingReleases(ctx context.Context) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("PendingReleases", 0, ctx)
	if s.listErr != nil {
		return nil, s.listErr
	}
	return slices.Clone(s.pending), nil
}

func (s *fakeStore) RemovePendingRelease(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("RemovePendingRelease", id, ctx)
	if s.removeErr != nil {
		if err := s.removeErr(id); err != nil {
			return err
		}
	}
	s.pending = slices.DeleteFunc(s.pending, func(p int64) bool { return p == id })
	return nil
}

func (s *fakeStore) methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.calls {
		out = append(out, c.method)
	}
	return out
}

func (s *fakeStore) find(method string) []call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []call
	for _, c := range s.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func outOfStockErr() error {
	st, err := status.New(codes.FailedPrecondition, "out of stock").WithDetails(
		&errdetails.ErrorInfo{Reason: "OUT_OF_STOCK", Domain: "warehouse.example"})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func TestCreateOrderSuccess(t *testing.T) {
	st, wh := &fakeStore{}, &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	in := []Item{{ProductID: 3, Quantity: 2}, {ProductID: 1, Quantity: 1}}
	o, err := s.CreateOrder(context.Background(), 42, in)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	want := []Item{{ProductID: 1, Quantity: 1}, {ProductID: 3, Quantity: 2}}
	if o.ID != 1 || o.CustomerID != 42 || !slices.Equal(o.Items, want) {
		t.Fatalf("заказ = %+v, want ID 1, CustomerID 42, Items %v", o, want)
	}
	if got := wh.methods(); !slices.Equal(got, []string{"Reserve"}) {
		t.Fatalf("вызовы склада = %v, want [Reserve]", got)
	}
	if !slices.Equal(wh.items[0], want) {
		t.Fatalf("Reserve получил %v, want %v (по возрастанию ProductID)", wh.items[0], want)
	}
	if got := st.methods(); !slices.Equal(got, []string{"NextID", "Save"}) {
		t.Fatalf("вызовы хранилища = %v, want [NextID Save]", got)
	}
}

func TestCreateOrderInvalid(t *testing.T) {
	st, wh := &fakeStore{}, &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	_, err := s.CreateOrder(context.Background(), 42, nil)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if len(st.methods())+len(wh.methods()) != 0 {
		t.Fatalf("при ошибке валидации были вызовы: %v %v", st.methods(), wh.methods())
	}
}

func TestCreateOrderOutOfStock(t *testing.T) {
	st := &fakeStore{}
	wh := &fakeWarehouse{reserve: func(context.Context, int64) error { return outOfStockErr() }}
	s := &Saga{Store: st, Warehouse: wh}
	o, err := s.CreateOrder(context.Background(), 42, []Item{{ProductID: 1, Quantity: 5}})
	if !errors.Is(err, ErrOutOfStock) {
		t.Fatalf("err = %v, want ErrOutOfStock", err)
	}
	if o.ID != 0 || o.Items != nil {
		t.Fatalf("при ошибке заказ должен быть нулевым, got %+v", o)
	}
	if got := wh.methods(); !slices.Equal(got, []string{"Reserve"}) {
		t.Fatalf("вызовы склада = %v: точный отказ компенсировать не нужно", got)
	}
}

func TestCreateOrderUnavailableCompensates(t *testing.T) {
	st := &fakeStore{}
	down := status.Error(codes.Unavailable, "warehouse is down")
	wh := &fakeWarehouse{reserve: func(context.Context, int64) error { return down }}
	s := &Saga{Store: st, Warehouse: wh}
	_, err := s.CreateOrder(context.Background(), 42, []Item{{ProductID: 1, Quantity: 1}})
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, down) {
		t.Fatalf("err = %v, want ErrUnavailable и исходную ошибку Reserve", err)
	}
	rel := wh.find("Release")
	if len(rel) != 1 || rel[0].id != 1 {
		t.Fatalf("Release = %+v, want один вызов для заказа 1", rel)
	}
}

func TestCreateOrderSaveFailsCompensates(t *testing.T) {
	boom := errors.New("disk I/O error")
	st := &fakeStore{save: func(Order) error { return boom }}
	wh := &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	_, err := s.CreateOrder(context.Background(), 42, []Item{{ProductID: 1, Quantity: 1}})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want ошибку Save", err)
	}
	if got := wh.methods(); !slices.Equal(got, []string{"Reserve", "Release"}) {
		t.Fatalf("вызовы склада = %v, want [Reserve Release]", got)
	}
}

func TestRetryReleasesBasic(t *testing.T) {
	st := &fakeStore{pending: []int64{7, 4}}
	wh := &fakeWarehouse{}
	s := &Saga{Store: st, Warehouse: wh}
	n, err := s.RetryReleases(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("RetryReleases = %d, %v; want 2, nil", n, err)
	}
	if len(st.pending) != 0 {
		t.Fatalf("очередь после повторов = %v, want пусто", st.pending)
	}
}

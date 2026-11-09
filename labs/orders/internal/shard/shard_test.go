package shard_test

import (
	"errors"
	"math"
	"path/filepath"
	"testing"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/shard"
	"gotutor/labs/orders/internal/store/sqlite"
	"gotutor/labs/orders/internal/storetest"
)

func TestJumpIsStable(t *testing.T) {
	// Значения зафиксированы (сверены с независимой реализацией алгоритма
	// из статьи): смена функции молча перетасовала бы покупателей по
	// шардам, и их история «пропала» бы.
	for _, tc := range []struct {
		key     uint64
		buckets int
		want    int
	}{
		{0, 1, 0},
		{0, 100, 0},
		{1, 100, 55},
		{0xdeadbeef, 100, 87},
		{math.MaxUint64, 1000, 313},
	} {
		if got := shard.Jump(tc.key, tc.buckets); got != tc.want {
			t.Errorf("Jump(%#x, %d) = %d, want %d", tc.key, tc.buckets, got, tc.want)
		}
	}
	// Покупатели 1..10 при двух и четырёх шардах.
	want2 := []int{0, 0, 1, 0, 0, 0, 1, 0, 1, 0}
	want4 := []int{3, 0, 1, 3, 3, 0, 1, 0, 2, 2}
	for c := int64(1); c <= 10; c++ {
		if got := shard.OfCustomer(c, 2); got != want2[c-1] {
			t.Errorf("OfCustomer(%d, 2) = %d, want %d", c, got, want2[c-1])
		}
		if got := shard.OfCustomer(c, 4); got != want4[c-1] {
			t.Errorf("OfCustomer(%d, 4) = %d, want %d", c, got, want4[c-1])
		}
	}
	if got := shard.OfCustomer(12345, 1); got != 0 {
		t.Errorf("OfCustomer with one shard = %d, want 0", got)
	}
}

func TestJumpDistributionAndMinimalMovement(t *testing.T) {
	const customers = 30000
	counts := make([]int, 3)
	moved := 0
	for c := int64(1); c <= customers; c++ {
		before, after := shard.OfCustomer(c, 2), shard.OfCustomer(c, 3)
		counts[after]++
		if before != after {
			moved++
			if after != 2 {
				t.Fatalf("customer %d moved %d → %d: при добавлении шарда переезжают только на новый", c, before, after)
			}
		}
	}
	for i, n := range counts {
		if n < customers/3*9/10 || n > customers/3*11/10 {
			t.Errorf("shard %d has %d of %d customers: want ≈1/3 ± 10%%", i, n, customers)
		}
	}
	if moved < customers/3*9/10 || moved > customers/3*11/10 {
		t.Errorf("moved %d of %d: want ≈1/3", moved, customers)
	}
}

func newStore(t *testing.T, name string) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRouterOrderIDs(t *testing.T) {
	s0, s1 := newStore(t, "s0.db"), newStore(t, "s1.db")
	r, err := shard.New([]orders.Store{s0, s1})
	if err != nil {
		t.Fatal(err)
	}
	// Покупатели обоих шардов.
	var c0, c1 int64
	for c := int64(1); c0 == 0 || c1 == 0; c++ {
		if r.ShardOf(c) == 0 && c0 == 0 {
			c0 = c
		} else if r.ShardOf(c) == 1 && c1 == 0 {
			c1 = c
		}
	}
	id0, err := r.ForCustomer(c0).NextOrderID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	id1, err := r.ForCustomer(c1).NextOrderID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id0 != 1 {
		t.Errorf("shard 0 id = %d, want 1: номера шарда 0 не меняются", id0)
	}
	if id1 != 1<<48|1 || shard.OfOrder(id1) != 1 {
		t.Errorf("shard 1 id = %d, want %d", id1, int64(1<<48|1))
	}
	for _, id := range []int64{2 << 48, -1, 0} {
		if _, err := r.ForOrder(id); !errors.Is(err, orders.ErrNotFound) {
			t.Errorf("ForOrder(%d) = %v, want ErrNotFound", id, err)
		}
	}
	if _, err := shard.New(nil); err == nil {
		t.Error("New(nil): want error")
	}
}

// TestServiceOnShards — сервис с двумя шардами: заказы ложатся на шард
// покупателя, GetOrder и CancelOrder находят их по номеру, история
// покупателя — с его шарда.
func TestServiceOnShards(t *testing.T) {
	s0, s1 := newStore(t, "s0.db"), newStore(t, "s1.db")
	// Каталог — справочник, одинаковый на всех шардах (позиции ссылаются
	// на products внешним ключом).
	var pen orders.Product
	for _, s := range []*sqlite.Store{s0, s1} {
		p, err := s.CreateProduct(t.Context(), orders.NewProduct{Name: "Ручка", PriceCents: 100})
		if err != nil {
			t.Fatal(err)
		}
		pen = p
	}
	r, err := shard.New([]orders.Store{s0, s1})
	if err != nil {
		t.Fatal(err)
	}
	svc := orders.NewService(s0, storetest.NewWarehouse(), orders.Options{Router: r})

	perShard := map[int]int{}
	for c := int64(1); c <= 20; c++ {
		o, err := svc.CreateOrder(t.Context(), orders.CreateOrderRequest{
			CustomerID: c, Items: []orders.ItemRequest{{ProductID: pen.ID, Quantity: 1}},
		})
		if err != nil {
			t.Fatalf("CreateOrder(customer %d): %v", c, err)
		}
		want := r.ShardOf(c)
		if shard.OfOrder(o.ID) != want {
			t.Errorf("order %d of customer %d on shard %d, want %d", o.ID, c, shard.OfOrder(o.ID), want)
		}
		perShard[want]++
		// Заказ лежит именно в базе своего шарда.
		raw := []*sqlite.Store{s0, s1}[want]
		if _, err := raw.Order(t.Context(), o.ID); err != nil {
			t.Errorf("order %d not in shard %d: %v", o.ID, want, err)
		}
		if got, err := svc.Order(t.Context(), o.ID); err != nil || got.CustomerID != c {
			t.Errorf("Order(%d) = %+v, %v", o.ID, got, err)
		}
		list, _, err := svc.ListOrders(t.Context(), c, 0, 10)
		if err != nil || len(list) != 1 || list[0].ID != o.ID {
			t.Errorf("ListOrders(%d) = %+v, %v", c, list, err)
		}
		if got, err := svc.CancelOrder(t.Context(), o.ID); err != nil || got.Status != orders.StatusCancelled {
			t.Errorf("CancelOrder(%d) = %+v, %v", o.ID, got, err)
		}
	}
	if perShard[0] == 0 || perShard[1] == 0 {
		t.Errorf("orders per shard = %v, want both shards used", perShard)
	}
}

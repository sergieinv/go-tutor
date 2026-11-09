package postgres_test

import (
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/replica"
	"gotutor/labs/orders/internal/store/postgres"
	"gotutor/labs/orders/internal/storetest"
)

// TestStore запускает контрактный набор против настоящего PostgreSQL.
//
// ORDERS_TEST_POSTGRES — URL с правами создавать базы, например
// postgres://postgres:labs@127.0.0.1:55432/postgres?sslmode=disable.
// Каждый подтест получает свою базу orders_test_<случайное> и удаляет её.
func TestStore(t *testing.T) {
	adminDSN := os.Getenv("ORDERS_TEST_POSTGRES")
	if adminDSN == "" {
		t.Skip("ORDERS_TEST_POSTGRES is not set")
	}
	storetest.Run(t, func(t *testing.T) storetest.Store {
		return newTestStore(t, adminDSN)
	})
}

func newTestStore(t *testing.T, adminDSN string) *postgres.Store {
	t.Helper()
	s, _ := newTestDB(t, adminDSN)
	return s
}

// newTestDB создаёт временную базу и возвращает хранилище и имя базы.
func newTestDB(t *testing.T, adminDSN string) (*postgres.Store, string) {
	t.Helper()
	ctx := t.Context()

	u, err := url.Parse(adminDSN)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("ORDERS_TEST_POSTGRES must be a postgres:// URL")
	}
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}

	// Имя базы нельзя передать плейсхолдером ($1 — только для значений).
	// Поэтому оно строится из безопасных символов и всё равно экранируется.
	name := "orders_test_" + strings.ToLower(rand.Text())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		admin.Close(ctx)
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := storetest.CleanupContext()
		defer cancel()
		defer admin.Close(ctx)
		// WITH (FORCE) обрывает соединения, если кто-то их не закрыл.
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})

	u.Path = "/" + name
	s, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close) // выполнится раньше DROP DATABASE: t.Cleanup — LIFO
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s, name
}

// TestReplicaReadYourWrites — токен согласованности на настоящей потоковой
// реплике (postgres-replica, задержка применения 200 мс).
//
// ORDERS_TEST_POSTGRES_REPLICA — URL реплики, например
// postgres://postgres:labs@127.0.0.1:55433/postgres?sslmode=disable.
// Тестовая база создаётся на основной и приходит на реплику с WAL.
func TestReplicaReadYourWrites(t *testing.T) {
	adminDSN, replicaDSN := os.Getenv("ORDERS_TEST_POSTGRES"), os.Getenv("ORDERS_TEST_POSTGRES_REPLICA")
	if adminDSN == "" || replicaDSN == "" {
		t.Skip("ORDERS_TEST_POSTGRES and ORDERS_TEST_POSTGRES_REPLICA are not set")
	}
	ctx := t.Context()
	primary, dbName := newTestDB(t, adminDSN)

	u, err := url.Parse(replicaDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + dbName
	rep, err := postgres.OpenReplica(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rep.Close)

	if _, err := primary.ReplayPosition(ctx); err == nil {
		t.Error("ReplayPosition on primary: want error (not a replica)")
	}

	h, err := replica.New(replica.Config{Primaries: []replica.Primary{primary}, Replica: rep})
	if err != nil {
		t.Fatal(err)
	}
	pen, err := primary.CreateProduct(ctx, orders.NewProduct{Name: "Ручка", PriceCents: 100})
	if err != nil {
		t.Fatal(err)
	}
	svc := orders.NewService(primary, storetest.NewWarehouse(), orders.Options{History: h})
	// Ждём, пока база и товар доедут до реплики (иначе реплика «не видит»
	// базу, и чтение честно уходит на основную).
	waitReplay(t, primary, rep)

	o, err := svc.CreateOrder(ctx, orders.CreateOrderRequest{CustomerID: 7, Items: []orders.ItemRequest{{ProductID: pen.ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	tok := svc.WritePosition(ctx, 7)
	if !strings.HasPrefix(string(tok), "lsn:") {
		t.Fatalf("token = %q, want lsn:…", tok)
	}

	// С токеном заказ виден сразу, хотя реплика отстаёт на 200 мс.
	list, _, err := svc.ListOrdersAfter(ctx, 7, 0, 10, tok)
	if err != nil || len(list) != 1 || list[0].ID != o.ID {
		t.Fatalf("ListOrdersAfter(token) = %+v, %v; want the new order", list, err)
	}
	// Напрямую с реплики сразу после записи заказа обычно ещё нет (это не
	// проверяется строго: на медленной машине реплика могла успеть).
	if stale, err := rep.ListOrders(ctx, 7, 0, 10); err == nil && len(stale) == 0 {
		t.Log("replica is behind right after the write, as expected")
	}
	waitReplay(t, primary, rep)
	if fresh, err := rep.ListOrders(ctx, 7, 0, 10); err != nil || len(fresh) != 1 {
		t.Errorf("replica after catch-up = %+v, %v; want the order", fresh, err)
	}
}

// waitReplay ждёт, пока реплика применит текущую позицию основной (не
// дольше 10 с). Опрос, а не пауза: реплика может догнать и быстрее.
func waitReplay(t *testing.T, primary, rep *postgres.Store) {
	t.Helper()
	want, err := primary.WALPosition(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got, err := rep.ReplayPosition(t.Context()); err == nil && got >= want {
			// База могла ещё не открыться для подключений — проверяем.
			if err := rep.Ping(t.Context()); err == nil {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("replica did not reach %d in 10s", want)
}

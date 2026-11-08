// Внешний тестовый пакет: тест видит только публичный API,
// как любой другой потребитель, и не ломается от
// рефакторинга внутренностей.

package orders_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	orders "lesson"
)

// Фейк — рабочая реализация в памяти, а не мок с ожиданиями
// вызовов. Тесты проверяют результат и состояние, поэтому
// не ломаются, если сервис начнёт сохранять иначе.
type fakeRepo struct {
	mu   sync.Mutex
	byID map[string]orders.Order
}

var _ orders.Repo = (*fakeRepo)(nil)

func newFakeRepo(seed ...orders.Order) *fakeRepo {
	r := &fakeRepo{byID: map[string]orders.Order{}}
	for _, o := range seed {
		r.byID[o.ID] = o
	}
	return r
}

func (r *fakeRepo) Save(_ context.Context, o orders.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.byID[o.ID]; dup {
		return fmt.Errorf("дубликат id %q", o.ID)
	}
	r.byID[o.ID] = o
	return nil
}

// Обход map идёт в случайном порядке — и хорошо: фейк, как
// и БД, не обещает порядок, и тест, который на него
// полагается, падает сразу, а не раз в неделю в CI.
func (r *fakeRepo) ByCustomer(_ context.Context, c string) ([]orders.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var list []orders.Order
	for _, o := range r.byID {
		if o.Customer == c {
			list = append(list, o)
		}
	}
	return list, nil
}

// Builder: валидный заказ с разумными значениями по
// умолчанию. Тест меняет только то, что важно для него, —
// и при новом поле в `Order` правится одна функция.
var t0 = time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)

func anOrder(mods ...func(*orders.Order)) orders.Order {
	o := orders.Order{ID: "ord-1", Customer: "alice", Total: 100_00, Created: t0}
	for _, m := range mods {
		m(&o)
	}
	return o
}

// Сервис с фиксированными часами и последовательными id:
// никакого `time.Now` и случайности — результат одинаков
// при каждом запуске и под `-count=100`.
func newService(t *testing.T, repo orders.Repo) *orders.Service {
	t.Helper()
	n := 0
	ids := func() string { n++; return fmt.Sprintf("ord-%d", n) }
	return orders.NewService(repo, func() time.Time { return t0 }, ids)
}

// Сообщение называет вызов, входные данные, got и want —
// по нему ясно, что сломалось, без чтения теста.
func TestPlace(t *testing.T) {
	repo := newFakeRepo()
	got, err := newService(t, repo).Place(t.Context(), " alice ", 250_00)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	want := anOrder(func(o *orders.Order) { o.Total = 250_00 })
	if got != want {
		t.Errorf(`Place(" alice ", 25000) = %+v, want %+v`, got, want)
	}
	if saved := repo.byID[want.ID]; saved != want {
		t.Errorf("в хранилище %+v, want %+v", saved, want)
	}
}

func TestPlaceInvalid(t *testing.T) {
	tests := []struct {
		name, customer string
		total          int
	}{
		{"пустой клиент", "  ", 100},
		{"нулевая сумма", "bob", 0},
		{"отрицательная сумма", "bob", -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			_, err := newService(t, repo).Place(t.Context(), tt.customer, tt.total)
			if !errors.Is(err, orders.ErrInvalid) {
				t.Errorf("Place(%q, %d) error = %v, want ErrInvalid", tt.customer, tt.total, err)
			}
			if len(repo.byID) != 0 {
				t.Errorf("Place(%q, %d) сохранил заказ, хотя не должен", tt.customer, tt.total)
			}
		})
	}
}

// Данные — через builder: видно, чем заказы отличаются.
// Два заказа с одинаковым временем проверяют второй ключ.
func TestHistory(t *testing.T) {
	repo := newFakeRepo(
		anOrder(func(o *orders.Order) { o.ID = "b"; o.Created = t0.Add(time.Hour) }),
		anOrder(func(o *orders.Order) { o.ID = "c" }),
		anOrder(func(o *orders.Order) { o.ID = "a" }),
		anOrder(func(o *orders.Order) { o.ID = "z"; o.Customer = "bob" }),
	)
	list, err := newService(t, repo).History(t.Context(), "alice")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var got []string
	for _, o := range list {
		got = append(got, o.ID)
	}
	if want := []string{"a", "c", "b"}; !slices.Equal(got, want) {
		t.Errorf("History(alice) ids = %v, want %v", got, want)
	}
}

// Интеграционный тест запускается только там, где есть БД:
// CI задаёт переменную в отдельном job, а `go test -short`
// пропускает его на машине разработчика.
func TestRepoIntegration(t *testing.T) {
	dsn := os.Getenv("ORDERS_TEST_DSN")
	if testing.Short() || dsn == "" {
		t.Skip("интеграционный: нужна ORDERS_TEST_DSN и запуск без -short")
	}
	// Дальше в проекте: своя схема на тест, настоящий
	// репозиторий и те же проверки, что и для фейка.
	t.Logf("подключение к %s", dsn)
}

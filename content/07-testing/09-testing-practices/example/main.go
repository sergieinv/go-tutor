// Маленький сервис заказов, написанный так, чтобы его было
// легко тестировать: всё, что меняется от запуска к запуску
// (время, идентификаторы, хранилище), приходит снаружи.

package orders

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Order struct {
	ID       string
	Customer string
	Total    int // в копейках
	Created  time.Time
}

var ErrInvalid = errors.New("orders: неверный заказ")

// Интерфейс объявляет потребитель, и в нём только то, что
// сервису нужно. Порядок результата `ByCustomer` не
// обещан — как у SQL-запроса без `ORDER BY`.
type Repo interface {
	Save(ctx context.Context, o Order) error
	ByCustomer(ctx context.Context, customer string) ([]Order, error)
}

// Часы и генератор идентификаторов — обычные функции. В
// `main` сюда передают `time.Now` и, например, `rand.Text`.
type Service struct {
	repo  Repo
	now   func() time.Time
	newID func() string
}

func NewService(repo Repo, now func() time.Time, newID func() string) *Service {
	return &Service{repo: repo, now: now, newID: newID}
}

func (s *Service) Place(ctx context.Context, customer string, total int) (Order, error) {
	customer = strings.TrimSpace(customer)
	if customer == "" || total <= 0 {
		return Order{}, fmt.Errorf("%w: клиент %q, сумма %d", ErrInvalid, customer, total)
	}
	o := Order{ID: s.newID(), Customer: customer, Total: total, Created: s.now()}
	if err := s.repo.Save(ctx, o); err != nil {
		return Order{}, fmt.Errorf("orders: сохранение: %w", err)
	}
	return o, nil
}

// История клиента — в стабильном порядке: по времени, при
// равном времени — по ID. Без второго ключа порядок
// зависел бы от хранилища.
func (s *Service) History(ctx context.Context, customer string) ([]Order, error) {
	list, err := s.repo.ByCustomer(ctx, customer)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(list, func(a, b Order) int {
		return cmp.Or(a.Created.Compare(b.Created), cmp.Compare(a.ID, b.ID))
	})
	return list, nil
}

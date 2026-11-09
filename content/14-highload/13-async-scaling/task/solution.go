package task

import (
	"errors"
	"slices"
	"strings"
	"sync"
)

// ErrBadEvent — событие нарушает формат.
var ErrBadEvent = errors.New("bad event")

// Event — событие заказа. Seq — номер события в пределах заказа:
// 1, 2, 3, … без пропусков.
type Event struct {
	OrderID  string
	Seq      int
	Kind     string // "created", "item_added", "paid", "cancelled"
	Customer string // для "created"
	Amount   int64  // для "item_added"
}

// OrderView — строка модели чтения.
type OrderView struct {
	OrderID  string
	Customer string
	Status   string // "new", "paid", "cancelled"
	Total    int64
	Seq      int // номер последнего применённого события
}

// Stats — счётчики проекции.
type Stats struct {
	Applied    int // применено событий
	Duplicates int // отброшено повторов
	Pending    int // ждут пропущенных предшественников сейчас
}

// Projection строит модель чтения заказов из потока событий,
// который может дублировать и переставлять события.
type Projection struct {
	mu      sync.Mutex
	orders  map[string]*OrderView
	pending map[string]map[int]Event // заказ → номер → событие
	stats   Stats
}

func NewProjection() *Projection {
	return &Projection{orders: map[string]*OrderView{}, pending: map[string]map[int]Event{}}
}

func validate(e Event) error {
	if e.OrderID == "" || e.Seq < 1 {
		return ErrBadEvent
	}
	switch e.Kind {
	case "created":
		if e.Seq != 1 || e.Customer == "" {
			return ErrBadEvent
		}
	case "item_added", "paid", "cancelled":
		if e.Seq == 1 {
			return ErrBadEvent
		}
	default:
		return ErrBadEvent
	}
	return nil
}

// Apply принимает событие: применяет, откладывает или отбрасывает
// как повтор.
func (p *Projection) Apply(e Event) error {
	if err := validate(e); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	last := 0
	if v, ok := p.orders[e.OrderID]; ok {
		last = v.Seq
	}
	if e.Seq <= last {
		p.stats.Duplicates++
		return nil
	}
	if _, ok := p.pending[e.OrderID][e.Seq]; ok {
		p.stats.Duplicates++
		return nil
	}
	if e.Seq > last+1 {
		if p.pending[e.OrderID] == nil {
			p.pending[e.OrderID] = map[int]Event{}
		}
		p.pending[e.OrderID][e.Seq] = e
		p.stats.Pending++
		return nil
	}
	p.apply(e)
	// Дождавшиеся своей очереди отложенные события — следом.
	for {
		next, ok := p.pending[e.OrderID][e.Seq+1]
		if !ok {
			break
		}
		delete(p.pending[e.OrderID], next.Seq)
		p.stats.Pending--
		p.apply(next)
		e = next
	}
	if len(p.pending[e.OrderID]) == 0 {
		delete(p.pending, e.OrderID)
	}
	return nil
}

func (p *Projection) apply(e Event) {
	v := p.orders[e.OrderID]
	switch e.Kind {
	case "created":
		v = &OrderView{OrderID: e.OrderID, Customer: e.Customer, Status: "new"}
		p.orders[e.OrderID] = v
	case "item_added":
		v.Total += e.Amount
	case "paid":
		v.Status = "paid"
	case "cancelled":
		v.Status = "cancelled"
	}
	v.Seq = e.Seq
	p.stats.Applied++
}

// Order возвращает заказ из модели чтения.
func (p *Projection) Order(id string) (OrderView, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.orders[id]
	if !ok {
		return OrderView{}, false
	}
	return *v, true
}

// ByCustomer — заказы покупателя по возрастанию OrderID.
func (p *Projection) ByCustomer(customer string) []OrderView {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []OrderView
	for _, v := range p.orders {
		if v.Customer == customer {
			out = append(out, *v)
		}
	}
	slices.SortFunc(out, func(a, b OrderView) int { return strings.Compare(a.OrderID, b.OrderID) })
	return out
}

// Stats возвращает счётчики.
func (p *Projection) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

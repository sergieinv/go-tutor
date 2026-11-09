package task

import "errors"

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
	// TODO
}

func NewProjection() *Projection {
	// TODO
	return &Projection{}
}

// Apply принимает событие: применяет, откладывает или отбрасывает
// как повтор.
func (p *Projection) Apply(e Event) error {
	// TODO
	return nil
}

// Order возвращает заказ из модели чтения.
func (p *Projection) Order(id string) (OrderView, bool) {
	// TODO
	return OrderView{}, false
}

// ByCustomer — заказы покупателя по возрастанию OrderID.
func (p *Projection) ByCustomer(customer string) []OrderView {
	// TODO
	return nil
}

// Stats возвращает счётчики.
func (p *Projection) Stats() Stats {
	// TODO
	return Stats{}
}

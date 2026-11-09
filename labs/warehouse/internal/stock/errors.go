package stock

import (
	"errors"
	"fmt"
	"strings"
)

// Доменные ошибки. Транспорт (grpcapi) переводит их в коды gRPC с
// деталями в одном месте; сам домен про коды ничего не знает.
var (
	// ErrReservationReleased — резерв заказа уже отменён (в том числе
	// «надгробием»: Release пришёл раньше Reserve).
	ErrReservationReleased = errors.New("reservation already released")
	// ErrReservationMismatch — у заказа уже есть действующий резерв с
	// другими позициями.
	ErrReservationMismatch = errors.New("reservation exists with different items")
)

// Shortage — нехватка по одной позиции.
type Shortage struct {
	ProductID int64
	Requested int32
	Available int32
}

// OutOfStockError — не хватает товара хотя бы по одной позиции. Shortages
// перечисляет все нехватающие позиции (по ProductID), а не первую: клиент
// покажет покупателю сразу всё, что придётся убрать из корзины.
//
// Ошибка-тип, а не sentinel: у неё есть данные. Проверка — errors.As, как
// catch (OutOfStockException e) с полями в C#.
type OutOfStockError struct {
	Shortages []Shortage
}

func (e *OutOfStockError) Error() string {
	parts := make([]string, len(e.Shortages))
	for i, s := range e.Shortages {
		parts[i] = fmt.Sprintf("product %d: requested %d, available %d", s.ProductID, s.Requested, s.Available)
	}
	return "out of stock: " + strings.Join(parts, "; ")
}

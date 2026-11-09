package orders

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Доменные ошибки. Хранилища, клиент склада и сервис оборачивают их
// (fmt.Errorf с %w), а вызывающий код проверяет через errors.Is — так же,
// как в C# ловят конкретный тип исключения, только без исключений.
var (
	// ErrNotFound — заказа нет.
	ErrNotFound = errors.New("not found")
	// ErrOutOfStock — на складе не хватает товара. Подробности — в
	// *OutOfStockError.
	ErrOutOfStock = errors.New("out of stock")
	// ErrConflict — операция противоречит текущему состоянию: например,
	// статус заказа успели сменить параллельно.
	ErrConflict = errors.New("conflict")
	// ErrWarehouseUnavailable — склад не ответил (недоступен, не уложился в
	// дедлайн, выключатель разомкнут). Временная ошибка: запрос можно
	// повторить позже.
	ErrWarehouseUnavailable = errors.New("warehouse unavailable")
)

// ValidationError — входные данные не прошли проверку. Тип, а не
// переменная: несёт поле и причину, которые безопасно показать клиенту.
// Проверяется через errors.As.
type ValidationError struct {
	Field   string // путь к полю: "items[1].product_id"
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Message)
}

// OutOfStockError — каких товаров не хватило. errors.Is(err, ErrOutOfStock)
// для неё истинно.
type OutOfStockError struct {
	ProductIDs []int64
}

func (e *OutOfStockError) Error() string {
	ids := make([]string, len(e.ProductIDs))
	for i, id := range e.ProductIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}
	return "out of stock: products " + strings.Join(ids, ", ")
}

// Unwrap связывает тип с ErrOutOfStock.
func (e *OutOfStockError) Unwrap() error { return ErrOutOfStock }

package orders

import (
	"errors"
	"fmt"
)

// Доменные ошибки. Хранилища и сервис оборачивают их (fmt.Errorf с %w),
// а вызывающий код проверяет через errors.Is — так же, как в C# ловят
// конкретный тип исключения, только без исключений.
var (
	// ErrNotFound — заказа или товара нет.
	ErrNotFound = errors.New("not found")
	// ErrOutOfStock — на складе не хватает товара.
	ErrOutOfStock = errors.New("out of stock")
	// ErrConflict — операция противоречит текущему состоянию: например,
	// статус заказа успели сменить параллельно.
	ErrConflict = errors.New("conflict")
)

// ValidationError — входные данные не прошли проверку. Тип, а не
// переменная: несёт поле и причину, которые безопасно показать клиенту.
// Проверяется через errors.As.
type ValidationError struct {
	Field   string // путь к полю: "items[1].quantity"
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Message)
}

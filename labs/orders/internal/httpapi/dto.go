package httpapi

import (
	"time"

	"gotutor/labs/orders/internal/orders"
)

// DTO — формат JSON на границе API. Деньги — целые копейки, время — RFC 3339
// в UTC (так кодирует time.Time пакет encoding/json).

type createOrderRequest struct {
	CustomerID int64             `json:"customer_id"`
	Items      []itemRequestJSON `json:"items"`
}

type itemRequestJSON struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
}

func (r createOrderRequest) toDomain() orders.CreateOrderRequest {
	items := make([]orders.ItemRequest, len(r.Items))
	for i, it := range r.Items {
		items[i] = orders.ItemRequest{ProductID: it.ProductID, Quantity: it.Quantity}
	}
	return orders.CreateOrderRequest{CustomerID: r.CustomerID, Items: items}
}

type orderResponse struct {
	ID         int64              `json:"id"`
	CustomerID int64              `json:"customer_id"`
	Status     string             `json:"status"`
	Items      []itemResponseJSON `json:"items"`
	TotalCents int64              `json:"total_cents"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

type itemResponseJSON struct {
	ProductID  int64 `json:"product_id"`
	Quantity   int   `json:"quantity"`
	PriceCents int64 `json:"price_cents"`
}

func newOrderResponse(o orders.Order) orderResponse {
	items := make([]itemResponseJSON, len(o.Items))
	for i, it := range o.Items {
		items[i] = itemResponseJSON{ProductID: it.ProductID, Quantity: it.Quantity, PriceCents: it.PriceCents}
	}
	return orderResponse{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Status:     string(o.Status),
		Items:      items,
		TotalCents: o.TotalCents,
		CreatedAt:  o.CreatedAt,
		UpdatedAt:  o.UpdatedAt,
	}
}

type productResponse struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	Available  int    `json:"available"`
}

func newProductResponse(p orders.Product) productResponse {
	return productResponse{ID: p.ID, Name: p.Name, PriceCents: p.PriceCents, Available: p.Available}
}

type statusResponse struct {
	Status string `json:"status"`
}

// errorResponse — тело любой ошибки. Code — машиночитаемый код, по нему
// клиент принимает решения; Message — для человека. Field — только для
// ошибок валидации. RequestID — чтобы найти запрос в журнале сервиса.
type errorResponse struct {
	Code      string `json:"error"`
	Message   string `json:"message"`
	Field     string `json:"field,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

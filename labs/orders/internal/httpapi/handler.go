// Package httpapi — JSON API сервиса «Заказы» на net/http.
//
// Маршрутизация — стандартный http.ServeMux с шаблонами Go 1.22
// ("POST /orders/{id}/cancel"): метод и параметры пути без сторонних
// роутеров. Формат JSON описан отдельными DTO-типами (dto.go), а не тегами
// на доменных структурах: API и домен меняются независимо.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"gotutor/labs/orders/internal/orders"
)

// Orders — то, что API нужно от домена. Интерфейс объявлен здесь, у
// потребителя: *orders.Service ему удовлетворяет, а тесты могут подставить
// свою реализацию.
type Orders interface {
	CreateOrder(ctx context.Context, req orders.CreateOrderRequest) (orders.Order, error)
	Order(ctx context.Context, id int64) (orders.Order, error)
	CancelOrder(ctx context.Context, id int64) (orders.Order, error)
	Product(ctx context.Context, id int64) (orders.Product, error)
}

// ReadyFunc проверяет готовность принимать трафик (в main — пинг базы).
type ReadyFunc func(ctx context.Context) error

const (
	// maxBodyBytes — предел тела запроса. Заказ на orders.MaxItems позиций
	// занимает несколько килобайт; всё, что больше, — ошибка или атака.
	maxBodyBytes = 64 << 10
	// requestTimeout — сколько обработчик может работать, включая запросы
	// к базе: контекст запроса с этим дедлайном доходит до драйвера.
	requestTimeout = 5 * time.Second
	// readyTimeout — сколько ждём ответа базы в /readyz.
	readyTimeout = time.Second
)

type handler struct {
	orders Orders
	ready  ReadyFunc
	log    *slog.Logger
}

// New собирает http.Handler со всеми маршрутами и middleware.
func New(svc Orders, ready ReadyFunc, log *slog.Logger) http.Handler {
	h := &handler{orders: svc, ready: ready, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", h.createOrder)
	mux.HandleFunc("GET /orders/{id}", h.getOrder)
	mux.HandleFunc("POST /orders/{id}/cancel", h.cancelOrder)
	mux.HandleFunc("GET /products/{id}", h.getProduct)
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)

	// Порядок снаружи внутрь: id запроса → дедлайн → журнал → перехват
	// паники → маршрутизатор. Журнал снаружи recover, чтобы запрос,
	// закончившийся паникой, попал в журнал со статусом 500. Между журналом
	// и mux нет r.WithContext: mux записывает шаблон маршрута в r.Pattern
	// того же *http.Request, который видит журнал.
	var next http.Handler = mux
	next = h.recoverPanic(next)
	next = h.logRequests(next)
	next = withTimeout(next, requestTimeout)
	next = withRequestID(next)
	return next
}

func (h *handler) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.fail(w, r, err)
		return
	}
	order, err := h.orders.CreateOrder(r.Context(), req.toDomain())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/orders/"+strconv.FormatInt(order.ID, 10))
	writeJSON(w, http.StatusCreated, newOrderResponse(order))
}

func (h *handler) getOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	order, err := h.orders.Order(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newOrderResponse(order))
}

func (h *handler) cancelOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	order, err := h.orders.CancelOrder(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newOrderResponse(order))
}

func (h *handler) getProduct(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p, err := h.orders.Product(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newProductResponse(p))
}

// healthz — liveness: процесс жив и обслуживает HTTP. Базу не трогает:
// если база недоступна, перезапуск сервиса не поможет.
func (h *handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

// readyz — readiness: можно ли слать трафик. Без базы — 503, и
// балансировщик уводит запросы на другие реплики.
func (h *handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()
	if err := h.ready(ctx); err != nil {
		h.log.WarnContext(ctx, "not ready", "err", err, "request_id", requestID(ctx))
		writeJSON(w, http.StatusServiceUnavailable, statusResponse{Status: "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

// pathID разбирает {id} из пути: положительное целое.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &requestError{status: http.StatusBadRequest, code: "invalid_id", message: "id must be a positive integer"}
	}
	return id, nil
}

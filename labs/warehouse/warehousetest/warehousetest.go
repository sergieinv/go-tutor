// Package warehousetest — настоящий «Склад» в памяти для тестов клиентов.
//
// Тесты «Заказов» поднимают его на bufconn или локальном порту и проверяют
// свою работу со «Складом» без MySQL и без docker: тот же домен
// stock.Service и тот же gRPC-сервер grpcapi с теми же interceptor'ами
// (валидация protovalidate, коды и детали ошибок), что у сервиса, — только
// хранилище в памяти. Это надёжнее рукописного мока: мок отвечает так, как
// автор теста думает, что отвечает сервис, а здесь отвечает сам сервис.
//
// Сбои соседа (недоступен, ответ потерян) внедряются через SetFault.
package warehousetest

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/platform/grpcx"
	"gotutor/labs/platform/logging"
	"gotutor/labs/warehouse/internal/grpcapi"
	"gotutor/labs/warehouse/internal/stock"
	"gotutor/labs/warehouse/internal/store/memstore"
)

// Phase — когда срабатывает внедрённая ошибка.
type Phase int

const (
	// Before — до обработки: сосед недоступен, операция не выполнена.
	Before Phase = iota
	// After — после успешной обработки: ответ потерян, операция выполнена.
	After
)

// Fault решает, вернуть ли ошибку вместо обычного поведения. method —
// короткое имя метода: "Reserve", "Release", "GetStock". nil — без ошибки.
// Ошибка возвращается клиенту как есть: передайте статус, например
// status.Error(codes.Unavailable, "warehouse is down").
type Fault func(method string, phase Phase) error

// Warehouse — «Склад» на хранилище в памяти. Безопасен для использования
// из разных горутин.
type Warehouse struct {
	store  *memstore.Store
	server *grpcapi.Server

	mu    sync.Mutex
	fault Fault
	calls map[string]int
}

// New создаёт склад с начальными остатками: product_id → available.
func New(stockLevels map[int64]int32) *Warehouse {
	store := memstore.New(stockLevels)
	svc := stock.NewService(store, time.Now, nil)
	return &Warehouse{
		store:  store,
		server: grpcapi.New(svc, slog.New(slog.DiscardHandler)),
		calls:  make(map[string]int),
	}
}

// SetFault задаёт внедрение ошибок; nil — без ошибок.
func (w *Warehouse) SetFault(f Fault) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fault = f
}

// Serve запускает gRPC-сервер на lis и останавливает его в t.Cleanup.
// Сервер собран так же, как в cmd/warehouse: grpcx.NewServer с валидацией
// protovalidate; журнал — в io.Discard.
func (w *Warehouse) Serve(t testing.TB, lis net.Listener) {
	t.Helper()
	v, err := protovalidate.New()
	if err != nil {
		t.Fatalf("warehousetest: protovalidate: %v", err)
	}
	srv := grpcx.NewServer(
		grpcx.ServerConfig{Logger: logging.New(io.Discard, slog.LevelInfo, "warehouse"), Validator: v},
		// grpc.UnaryInterceptor (в отличие от ChainUnaryInterceptor внутри
		// grpcx) встаёт первым в цепочку: счётчик видит каждый вызов, в том
		// числе отклонённый валидацией, а ошибка Before приходит раньше
		// всякой обработки — как от недоступного соседа.
		grpc.UnaryInterceptor(w.intercept),
	)
	warehousev1.RegisterWarehouseServiceServer(srv, w.server)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
}

// intercept считает вызовы и внедряет ошибки.
func (w *Warehouse) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	method := info.FullMethod[strings.LastIndex(info.FullMethod, "/")+1:]
	w.mu.Lock()
	w.calls[method]++
	fault := w.fault
	w.mu.Unlock()

	if fault != nil {
		if err := fault(method, Before); err != nil {
			return nil, err
		}
	}
	resp, err := handler(ctx, req)
	if err == nil && fault != nil {
		if ferr := fault(method, After); ferr != nil {
			return nil, ferr // операция выполнена, а клиент получил ошибку
		}
	}
	return resp, err
}

// Stock возвращает остаток товара (неизвестный — нули).
func (w *Warehouse) Stock(productID int64) (available, reserved int32) {
	l, _ := w.store.Level(context.Background(), productID)
	return l.Available, l.Reserved
}

// Reservation возвращает статус резерва заказа; ok == false — резерва нет.
func (w *Warehouse) Reservation(orderID int64) (status warehousev1.ReservationStatus, ok bool) {
	r, ok := w.store.Reservation(orderID)
	if !ok {
		return warehousev1.ReservationStatus_RESERVATION_STATUS_UNSPECIFIED, false
	}
	return grpcapi.StatusToProto(r.Status), true
}

// Calls — сколько вызовов method дошло до сервера, включая отклонённые
// Fault и валидацией.
func (w *Warehouse) Calls(method string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls[method]
}

package warehousetest_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/warehouse/warehousetest"
)

// start поднимает склад на bufconn и возвращает клиента — так же его
// будут использовать тесты «Заказов».
func start(t *testing.T, stock map[int64]int32) (*warehousetest.Warehouse, warehousev1.WarehouseServiceClient) {
	t.Helper()
	w := warehousetest.New(stock)
	lis := bufconn.Listen(1 << 20)
	w.Serve(t, lis)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return w, warehousev1.NewWarehouseServiceClient(conn)
}

func reserveReq(orderID, productID int64, qty int32) *warehousev1.ReserveRequest {
	return &warehousev1.ReserveRequest{
		OrderId: orderID,
		Items:   []*warehousev1.ReserveItem{{ProductId: productID, Quantity: qty}},
	}
}

func TestBehavesLikeWarehouse(t *testing.T) {
	w, c := start(t, map[int64]int32{1: 3})
	ctx := t.Context()

	if _, err := c.Reserve(ctx, reserveReq(10, 1, 2)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if a, r := w.Stock(1); a != 1 || r != 2 {
		t.Errorf("Stock(1) = %d, %d; want 1, 2", a, r)
	}
	if s, ok := w.Reservation(10); !ok || s != warehousev1.ReservationStatus_RESERVATION_STATUS_RESERVED {
		t.Errorf("Reservation(10) = %v, %v", s, ok)
	}

	// Настоящие коды ошибок домена…
	_, err := c.Reserve(ctx, reserveReq(11, 1, 5))
	if code := status.Code(err); code != codes.FailedPrecondition {
		t.Errorf("Reserve over stock: code %v, want FailedPrecondition", code)
	}
	// …и валидация контракта.
	_, err = c.Reserve(ctx, &warehousev1.ReserveRequest{OrderId: 12})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("Reserve without items: code %v, want InvalidArgument", code)
	}

	if _, err := c.Release(ctx, &warehousev1.ReleaseRequest{OrderId: 10}); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if s, _ := w.Reservation(10); s != warehousev1.ReservationStatus_RESERVATION_STATUS_RELEASED {
		t.Errorf("Reservation(10) after Release = %v", s)
	}
	if _, ok := w.Reservation(99); ok {
		t.Error("Reservation(99) exists, want none")
	}
	if got := w.Calls("Reserve"); got != 3 {
		t.Errorf("Calls(Reserve) = %d, want 3 (including rejected)", got)
	}
}

func TestFaultBefore(t *testing.T) {
	w, c := start(t, map[int64]int32{1: 3})
	w.SetFault(func(method string, phase warehousetest.Phase) error {
		if method == "Reserve" && phase == warehousetest.Before {
			return status.Error(codes.Unavailable, "warehouse is down")
		}
		return nil
	})

	_, err := c.Reserve(t.Context(), reserveReq(1, 1, 1))
	if code := status.Code(err); code != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable", code)
	}
	if a, r := w.Stock(1); a != 3 || r != 0 {
		t.Errorf("Stock(1) = %d, %d; want untouched 3, 0", a, r)
	}
	if _, ok := w.Reservation(1); ok {
		t.Error("reservation created despite Before fault")
	}
	if w.Calls("Reserve") != 1 {
		t.Errorf("Calls(Reserve) = %d, want 1", w.Calls("Reserve"))
	}
	// Другие методы не затронуты.
	if _, err := c.GetStock(t.Context(), &warehousev1.GetStockRequest{ProductId: 1}); err != nil {
		t.Errorf("GetStock: %v", err)
	}
}

func TestFaultAfter(t *testing.T) {
	w, c := start(t, map[int64]int32{1: 3})
	w.SetFault(func(method string, phase warehousetest.Phase) error {
		if method == "Reserve" && phase == warehousetest.After {
			return status.Error(codes.Unavailable, "response lost")
		}
		return nil
	})

	_, err := c.Reserve(t.Context(), reserveReq(1, 1, 2))
	if code := status.Code(err); code != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable", code)
	}
	// Ответ потерян, но резерв сделан: именно этот случай клиент обязан
	// пережить повтором с тем же order_id.
	if a, r := w.Stock(1); a != 1 || r != 2 {
		t.Errorf("Stock(1) = %d, %d; want 1, 2", a, r)
	}

	w.SetFault(nil)
	resp, err := c.Reserve(t.Context(), reserveReq(1, 1, 2))
	if err != nil {
		t.Fatalf("retry Reserve: %v", err)
	}
	if resp.GetReservation().GetStatus() != warehousev1.ReservationStatus_RESERVATION_STATUS_RESERVED {
		t.Errorf("retry = %v", resp.GetReservation())
	}
	if a, r := w.Stock(1); a != 1 || r != 2 {
		t.Errorf("Stock(1) after retry = %d, %d; want 1, 2 (no double reserve)", a, r)
	}
	if w.Calls("Reserve") != 2 {
		t.Errorf("Calls(Reserve) = %d, want 2", w.Calls("Reserve"))
	}
}

func TestFaultAfterNotAppliedToErrors(t *testing.T) {
	w, c := start(t, map[int64]int32{1: 1})
	w.SetFault(func(_ string, phase warehousetest.Phase) error {
		if phase == warehousetest.After {
			return status.Error(codes.Unavailable, "response lost")
		}
		return nil
	})
	// Обработка завершилась ошибкой домена — её и получает клиент.
	_, err := c.Reserve(t.Context(), reserveReq(1, 1, 5))
	if code := status.Code(err); code != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", code)
	}
}

package task

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// catalog — обработчики считают вызовы: невалидный запрос до них не доходит.
type catalog struct {
	UnimplementedCatalogServiceServer
	calls atomic.Int64
}

func (c *catalog) AddProduct(_ context.Context, req *AddProductRequest) (*AddProductResponse, error) {
	c.calls.Add(1)
	return &AddProductResponse{Sku: req.GetSku()}, nil
}

func (c *catalog) Rename(context.Context, *RenameRequest) (*RenameResponse, error) {
	c.calls.Add(1)
	return &RenameResponse{}, nil
}

func (c *catalog) Audit(context.Context, *AuditRequest) (*AuditResponse, error) {
	c.calls.Add(1)
	return &AuditResponse{}, nil
}

// start поднимает сервер с UnaryValidator на bufconn.
func start(t *testing.T) (CatalogServiceClient, *catalog) {
	t.Helper()
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnaryInterceptor(UnaryValidator(v)))
	impl := &catalog{}
	RegisterCatalogServiceServer(srv, impl)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewCatalogServiceClient(conn), impl
}

func badRequest(t *testing.T, err error) []*errdetails.BadRequest_FieldViolation {
	t.Helper()
	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument || st.Message() != "invalid request" {
		t.Fatalf("status = %v %q, want InvalidArgument \"invalid request\"", st.Code(), st.Message())
	}
	if len(st.Details()) != 1 {
		t.Fatalf("details = %v, want ровно один BadRequest", st.Details())
	}
	br, ok := st.Details()[0].(*errdetails.BadRequest)
	if !ok {
		t.Fatalf("detail %T, want *errdetails.BadRequest", st.Details()[0])
	}
	return br.GetFieldViolations()
}

func TestValidPasses(t *testing.T) {
	c, impl := start(t)
	resp, err := c.AddProduct(context.Background(), &AddProductRequest{Sku: "MUG-1", Title: "Кружка", PriceCents: 45000})
	if err != nil || resp.GetSku() != "MUG-1" {
		t.Fatalf("AddProduct = %v, %v", resp, err)
	}
	if impl.calls.Load() != 1 {
		t.Fatalf("вызовов обработчика = %d, want 1", impl.calls.Load())
	}
}

func TestInvalidSingleField(t *testing.T) {
	c, impl := start(t)
	_, err := c.AddProduct(context.Background(), &AddProductRequest{Sku: "MUG-1", Title: "Кружка", PriceCents: 0})
	fv := badRequest(t, err)
	if len(fv) != 1 || fv[0].GetField() != "price_cents" || fv[0].GetReason() != "int64.gt" || fv[0].GetDescription() == "" {
		t.Fatalf("violations = %v, want одно: price_cents int64.gt", fv)
	}
	if impl.calls.Load() != 0 {
		t.Fatal("обработчик вызван для невалидного запроса")
	}
}

func TestEveryMethodValidated(t *testing.T) {
	c, impl := start(t)
	_, err := c.Rename(context.Background(), &RenameRequest{Sku: "x", Title: "Новое"})
	fv := badRequest(t, err)
	if len(fv) != 1 || fv[0].GetField() != "sku" {
		t.Fatalf("violations = %v, want одно для sku", fv)
	}
	if impl.calls.Load() != 0 {
		t.Fatal("обработчик вызван для невалидного запроса")
	}
}

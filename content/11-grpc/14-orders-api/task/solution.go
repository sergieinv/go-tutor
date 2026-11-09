package task

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	idempotencyHeader = "idempotency-key"
	defaultPageSize   = 10
	maxKeyLen         = 64
)

type idemEntry struct {
	req *CreateOrderRequest
	id  string
}

// Service — реализация OrderService из orders.proto.
type Service struct {
	UnimplementedOrderServiceServer
	now func() time.Time

	mu     sync.Mutex
	orders []*Order          // в порядке создания
	byID   map[string]*Order // id → заказ
	idem   map[string]idemEntry
}

// NewService создаёт сервис; now — часы для create_time.
func NewService(now func() time.Time) *Service {
	return &Service{now: now, byID: map[string]*Order{}, idem: map[string]idemEntry{}}
}

func (s *Service) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	key, err := idempotencyKey(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != "" {
		if e, ok := s.idem[key]; ok {
			if !proto.Equal(e.req, req) {
				return nil, status.Error(codes.AlreadyExists, "idempotency key already used with a different request")
			}
			return &CreateOrderResponse{Order: proto.CloneOf(s.byID[e.id])}, nil
		}
	}
	o := &Order{
		Id:         fmt.Sprintf("ord-%d", len(s.orders)+1),
		CustomerId: req.GetCustomerId(),
		Items:      proto.CloneOf(req).GetItems(),
		Status:     OrderStatus_ORDER_STATUS_CREATED,
		CreateTime: timestamppb.New(s.now()),
	}
	for _, it := range o.Items {
		o.TotalCents += int64(it.GetQuantity()) * it.GetPriceCents()
	}
	s.orders = append(s.orders, o)
	s.byID[o.Id] = o
	if key != "" {
		s.idem[key] = idemEntry{req: proto.CloneOf(req), id: o.Id}
	}
	return &CreateOrderResponse{Order: proto.CloneOf(o)}, nil
}

// idempotencyKey читает ключ из metadata: нет — пустая строка, есть —
// ровно одно значение длиной 1..64.
func idempotencyKey(ctx context.Context) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	vals := md.Get(idempotencyHeader)
	switch {
	case len(vals) == 0:
		return "", nil
	case len(vals) > 1 || vals[0] == "" || len(vals[0]) > maxKeyLen:
		return "", status.Error(codes.InvalidArgument, "idempotency-key must be a single value of 1..64 characters")
	}
	return vals[0], nil
}

func (s *Service) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.byID[req.GetId()]
	if !ok {
		return nil, notFound(req.GetId())
	}
	return &GetOrderResponse{Order: proto.CloneOf(o)}, nil
}

func (s *Service) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error) {
	offset, err := decodeToken(req.GetPageToken())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid page_token")
	}
	size := int(req.GetPageSize())
	if size == 0 {
		size = defaultPageSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var mine []*Order
	for _, o := range s.orders {
		if o.GetCustomerId() == req.GetCustomerId() {
			mine = append(mine, o)
		}
	}
	if offset > len(mine) {
		return nil, status.Error(codes.InvalidArgument, "invalid page_token")
	}
	end := min(offset+size, len(mine))
	resp := &ListOrdersResponse{}
	for _, o := range mine[offset:end] {
		resp.Orders = append(resp.Orders, proto.CloneOf(o))
	}
	if end < len(mine) {
		resp.NextPageToken = encodeToken(end)
	}
	return resp, nil
}

// Токен страницы — непрозрачная для клиента строка: смещение с префиксом.
func encodeToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("off:" + strconv.Itoa(offset)))
}

func decodeToken(tok string) (int, error) {
	if tok == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return 0, err
	}
	num, ok := strings.CutPrefix(string(raw), "off:")
	if !ok {
		return 0, errors.New("bad token")
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return 0, errors.New("bad token")
	}
	return n, nil
}

func (s *Service) CancelOrder(ctx context.Context, req *CancelOrderRequest) (*CancelOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.byID[req.GetId()]
	if !ok {
		return nil, notFound(req.GetId())
	}
	if o.GetStatus() == OrderStatus_ORDER_STATUS_CANCELLED {
		return nil, status.Error(codes.FailedPrecondition, "order already cancelled")
	}
	o.Status = OrderStatus_ORDER_STATUS_CANCELLED
	return &CancelOrderResponse{Order: proto.CloneOf(o)}, nil
}

func notFound(id string) error {
	return status.Errorf(codes.NotFound, "order %q not found", id)
}

// validation — interceptor: правила из контракта для всех методов,
// нарушения → InvalidArgument + BadRequest.
func validation(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		m, ok := req.(proto.Message)
		if !ok {
			return h(ctx, req)
		}
		err := v.Validate(m)
		var verr *protovalidate.ValidationError
		switch {
		case err == nil:
			return h(ctx, req)
		case errors.As(err, &verr):
			br := &errdetails.BadRequest{}
			for _, viol := range verr.Violations {
				br.FieldViolations = append(br.FieldViolations, &errdetails.BadRequest_FieldViolation{
					Field:       protovalidate.FieldPathString(viol.Proto.GetField()),
					Description: viol.Proto.GetMessage(),
				})
			}
			st, _ := status.New(codes.InvalidArgument, "invalid request").WithDetails(br)
			return nil, st.Err()
		default:
			return nil, status.Error(codes.Internal, "internal error")
		}
	}
}

// NewGRPCServer создаёт gRPC-сервер с сервисом svc и interceptors.
func NewGRPCServer(svc *Service) *grpc.Server {
	v, err := protovalidate.New()
	if err != nil {
		panic(err) // правила контракта не компилируются — ошибка сборки, а не запроса
	}
	s := grpc.NewServer(
		grpc.MaxRecvMsgSize(1<<20),
		grpc.ChainUnaryInterceptor(validation(v)),
	)
	RegisterOrderServiceServer(s, svc)
	return s
}

// NewGateway возвращает REST-обработчик, который вызывает сервер через conn.
func NewGateway(ctx context.Context, conn *grpc.ClientConn) (http.Handler, error) {
	mux := runtime.NewServeMux(runtime.WithIncomingHeaderMatcher(func(h string) (string, bool) {
		if strings.EqualFold(h, idempotencyHeader) {
			return idempotencyHeader, true
		}
		return runtime.DefaultHeaderMatcher(h)
	}))
	if err := RegisterOrderServiceHandler(ctx, mux, conn); err != nil {
		return nil, err
	}
	return mux, nil
}

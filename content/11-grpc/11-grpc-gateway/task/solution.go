package task

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Server — сервис заказов в памяти. GetOrder и CancelOrder готовы.
type Server struct {
	UnimplementedOrderServiceServer
	mu     sync.Mutex
	orders []*Order
}

// NewServer копирует заказы; id уникальны.
func NewServer(orders ...*Order) *Server {
	s := &Server{}
	for _, o := range orders {
		s.orders = append(s.orders, proto.Clone(o).(*Order))
	}
	return s
}

func (s *Server) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.orders {
		if o.GetId() == req.GetId() {
			return &GetOrderResponse{Order: proto.Clone(o).(*Order)}, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "order %d not found", req.GetId())
}

func (s *Server) CancelOrder(ctx context.Context, req *CancelOrderRequest) (*CancelOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.orders {
		if o.GetId() != req.GetId() {
			continue
		}
		if o.GetStatus() == OrderStatus_ORDER_STATUS_CANCELLED {
			return nil, status.Errorf(codes.FailedPrecondition, "order %d already cancelled", req.GetId())
		}
		o.Status = OrderStatus_ORDER_STATUS_CANCELLED
		return &CancelOrderResponse{Order: proto.Clone(o).(*Order)}, nil
	}
	return nil, status.Errorf(codes.NotFound, "order %d not found", req.GetId())
}

// ListOrders отдаёт заказы с фильтрами по клиенту и статусу, по возрастанию id.
func (s *Server) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &ListOrdersResponse{}
	for _, o := range s.orders {
		if req.GetCustomerId() != "" && o.GetCustomerId() != req.GetCustomerId() {
			continue
		}
		if req.GetStatus() != OrderStatus_ORDER_STATUS_UNSPECIFIED && o.GetStatus() != req.GetStatus() {
			continue
		}
		resp.Orders = append(resp.Orders, proto.Clone(o).(*Order))
	}
	slices.SortFunc(resp.Orders, func(a, b *Order) int { return cmp.Compare(a.GetId(), b.GetId()) })
	return resp, nil
}

type errorBody struct {
	Error errorFields `json:"error"`
}

type errorFields struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// ErrorHandler отдаёт ошибку gateway в формате API.
func ErrorHandler(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
	httpStatus := 0
	var hse *runtime.HTTPStatusError
	if errors.As(err, &hse) {
		httpStatus, err = hse.HTTPStatus, hse.Err
	}
	st := status.Convert(err)
	if httpStatus == 0 {
		httpStatus = runtime.HTTPStatusFromCode(st.Code())
	}
	msg := st.Message()
	switch st.Code() {
	case codes.Internal, codes.Unknown, codes.DataLoss:
		msg = "internal error" // детали — в лог сервера, не клиенту
	}
	body := errorBody{Error: errorFields{
		Code:      code.Code(st.Code()).String(),
		Message:   msg,
		RequestID: r.Header.Get("X-Request-Id"),
	}}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(body)
}

// routingError переводит ошибки роутера gateway (нет пути, не тот метод) в ErrorHandler.
func routingError(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, httpStatus int) {
	c := codes.Internal
	switch httpStatus {
	case http.StatusNotFound:
		c = codes.NotFound
	case http.StatusMethodNotAllowed:
		c = codes.Unimplemented
	case http.StatusBadRequest:
		c = codes.InvalidArgument
	}
	ErrorHandler(ctx, mux, m, w, r, &runtime.HTTPStatusError{HTTPStatus: httpStatus, Err: status.Error(c, http.StatusText(httpStatus))})
}

// NewGateway собирает REST-обработчик поверх srv в том же процессе.
func NewGateway(ctx context.Context, srv OrderServiceServer) (http.Handler, error) {
	mux := runtime.NewServeMux(
		runtime.WithErrorHandler(ErrorHandler),
		runtime.WithRoutingErrorHandler(routingError),
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			if key == "X-Request-Id" {
				return "x-request-id", true
			}
			return runtime.DefaultHeaderMatcher(key)
		}),
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true},
		}),
	)
	if err := RegisterOrderServiceHandlerServer(ctx, mux, srv); err != nil {
		return nil, err
	}
	return limitBody(mux, 64<<10), nil
}

// limitBody читает тело целиком, не больше limit байт, до маршрутизации:
// декодер gateway читает только первое JSON-значение, и хвост из пробелов
// прошёл бы мимо http.MaxBytesReader.
func limitBody(mux *runtime.ServeMux, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if err != nil {
			ErrorHandler(r.Context(), mux, &runtime.JSONPb{}, w, r, status.Error(codes.InvalidArgument, "request body too large or unreadable"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mux.ServeHTTP(w, r)
	})
}

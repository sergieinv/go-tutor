// grpc-gateway генерирует из аннотаций `google.api.http`
// HTTP-обработчики: они разбирают путь, query и JSON-тело в
// protobuf-сообщение, вызывают gRPC-метод и отдают ответ как JSON.
// Пример поднимает gRPC-сервер и gateway на loopback и ходит
// в REST обычным `http.Client`.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// Сервис ничего не знает про HTTP: обычная реализация gRPC.
type orders struct {
	UnimplementedOrderServiceServer
	mu   sync.Mutex
	list []*Order
}

func (s *orders) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	if req.GetCustomerId() == "" || req.GetTotalCents() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "customer_id and positive total_cents required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := &Order{Id: int64(len(s.list) + 1), CustomerId: req.GetCustomerId(), TotalCents: req.GetTotalCents(), Status: OrderStatus_ORDER_STATUS_NEW}
	s.list = append(s.list, o)
	return &CreateOrderResponse{Order: o}, nil
}

func (s *orders) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.list {
		if o.GetId() == req.GetId() {
			return &GetOrderResponse{Order: o}, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "order %d not found", req.GetId())
}

func (s *orders) ListOrders(ctx context.Context, req *ListOrdersRequest) (*ListOrdersResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := &ListOrdersResponse{}
	for _, o := range s.list {
		if (req.GetCustomerId() == "" || o.GetCustomerId() == req.GetCustomerId()) &&
			(req.GetStatus() == OrderStatus_ORDER_STATUS_UNSPECIFIED || o.GetStatus() == req.GetStatus()) {
			resp.Orders = append(resp.Orders, o)
		}
	}
	return resp, nil
}

// Interceptor сервера показывает, что дошло до gRPC: заголовок
// `X-Request-Id` стал metadata `x-request-id`.
func logRID(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	fmt.Printf("  grpc: %s rid=%v\n", info.FullMethod[len("/shop.v1.OrderService/"):], md.Get("x-request-id"))
	return h(ctx, req)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// gRPC-сервер на loopback, порт выбирает ОС.
	glis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	gsrv := grpc.NewServer(grpc.ChainUnaryInterceptor(logRID))
	RegisterOrderServiceServer(gsrv, &orders{})
	go gsrv.Serve(glis)
	defer gsrv.Stop()

	// `runtime.ServeMux` — роутер gateway. Опции:
	// какие HTTP-заголовки пробрасывать в metadata (по умолчанию —
	// только `Grpc-Metadata-*` и `Authorization`), как печатать JSON
	// (имена полей как в .proto, нулевые значения не пропускать) и
	// что делать с ошибкой. Здесь ошибка уходит в стандартный
	// обработчик: HTTP-статус из кода gRPC и тело из `google.rpc.Status`.
	mux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			if key == "X-Request-Id" {
				return "x-request-id", true
			}
			return runtime.DefaultHeaderMatcher(key)
		}),
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions:   protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true},
			UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: false},
		}),
		runtime.WithErrorHandler(func(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
			fmt.Printf("  gateway: %s → %s\n", r.URL.Path, status.Code(err))
			runtime.DefaultHTTPErrorHandler(ctx, mux, m, w, r, err)
		}),
	)
	// `…HandlerFromEndpoint` — gateway ходит в gRPC по сети как
	// обычный клиент, и interceptors сервера срабатывают.
	err = RegisterOrderServiceHandlerFromEndpoint(ctx, mux, glis.Addr().String(),
		[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())})
	if err != nil {
		log.Fatal(err)
	}

	// HTTP-сервер — как в модуле 8: таймауты и лимит тела.
	hlis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	hsrv := &http.Server{
		Handler:           http.MaxBytesHandler(mux, 1<<20),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go hsrv.Serve(hlis)
	defer hsrv.Shutdown(context.Background())

	base := "http://" + hlis.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	// int64 в JSON — строка (`"1500"`): JavaScript не хранит
	// целые больше 2^53 точно. На входе gateway примет и число.
	call(client, "POST", base+"/v1/orders", `{"customer_id":"c1","total_cents":1500}`)
	call(client, "POST", base+"/v1/orders", `{"customer_id":"c2","total_cents":"700"}`)
	call(client, "GET", base+"/v1/orders/1", "")
	// Один метод — два маршрута (`additional_bindings`); enum в
	// query передаётся по имени.
	call(client, "GET", base+"/v1/customers/c2/orders", "")
	call(client, "GET", base+"/v1/orders?status=ORDER_STATUS_NEW&customer_id=c1", "")
	// Ошибки: код gRPC превращается в HTTP-статус.
	call(client, "GET", base+"/v1/orders/42", "")
	call(client, "POST", base+"/v1/orders", `{"customer_id":"c3"}`)
	call(client, "POST", base+"/v1/orders", `{"customer":"c3"}`)
	call(client, "DELETE", base+"/v1/orders/1", "")
}

// call отправляет запрос с X-Request-Id и печатает статус и тело.
// protojson намеренно меняет пробелы от сборки к сборке, чтобы на
// точный вид JSON не полагались, поэтому тело сжимаем `json.Compact`.
// По той же причине ошибки protobuf иногда пишут после «proto:»
// неразрывный пробел — заменяем его обычным.
func call(c *http.Client, method, url, body string) {
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("X-Request-Id", "r-1")
	path := url[strings.Index(url, "/v1"):]
	fmt.Println(strings.TrimSpace(method + " " + path + " " + body))
	resp, err := c.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		out.Reset()
		out.Write(bytes.TrimSpace(raw))
	}
	fmt.Printf("  → %d %s\n\n", resp.StatusCode, strings.ReplaceAll(out.String(), "\u00a0", " "))
}

package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Тесты с buf нужны buf и плагины protoc-gen-* рядом с ним (RUNNER_BUF
// или buf в PATH) и кеш зависимостей из buf.lock в BUF_CACHE_DIR (первый
// запуск с сетью наполнит его сам).
func newBufExecutor(t *testing.T) *Executor {
	t.Helper()
	bin := os.Getenv("RUNNER_BUF")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("buf"); err != nil {
			t.Skip("нет buf: go install github.com/bufbuild/buf/cmd/buf@v1.75.0 и плагины protoc-gen-go, protoc-gen-go-grpc, protoc-gen-grpc-gateway")
		}
	}
	e := newExecutor(t)
	e.cfg.Buf = bin
	e.cfg.BuildTimeout = 2 * e.cfg.BuildTimeout
	return e
}

const ordersProto = `syntax = "proto3";

package orders.v1;

import "buf/validate/validate.proto";
import "google/api/annotations.proto";

option go_package = "lesson/proto;main";

service OrderService {
  rpc GetOrder(GetOrderRequest) returns (GetOrderResponse) {
    option (google.api.http) = {get: "/v1/orders/{id}"};
  }
}

message GetOrderRequest {
  int64 id = 1 [(buf.validate.field).int64.gt = 0];
}

message GetOrderResponse {
  int64 id = 1;
  string customer = 2;
}
`

const ordersMain = `package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http/httptest"

	"buf.build/go/protovalidate"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type server struct {
	UnimplementedOrderServiceServer
	v protovalidate.Validator
}

func (s *server) GetOrder(ctx context.Context, r *GetOrderRequest) (*GetOrderResponse, error) {
	if err := s.v.Validate(r); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &GetOrderResponse{Id: r.GetId(), Customer: "ann"}, nil
}

func main() {
	v, err := protovalidate.New()
	if err != nil {
		panic(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	gs := grpc.NewServer()
	RegisterOrderServiceServer(gs, &server{v: v})
	go gs.Serve(lis)
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := RegisterOrderServiceHandlerFromEndpoint(context.Background(), mux, lis.Addr().String(), opts); err != nil {
		panic(err)
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()
	for _, p := range []string{"/v1/orders/7", "/v1/orders/0"} {
		resp, err := ts.Client().Get(ts.URL + p)
		if err != nil {
			panic(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		fmt.Println(p, resp.StatusCode)
	}
}
`

func TestProtoGenerateAndRun(t *testing.T) {
	e := newBufExecutor(t)
	// Устаревший сгенерированный файл от клиента должен быть заменён.
	files := Files{"main.go": ordersMain, "orders.proto": ordersProto, "orders.pb.go": "package main\n\nvar broken = \n"}
	res, err := e.Run(context.Background(), files, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Stdout != "/v1/orders/7 200\n/v1/orders/0 400\n" {
		t.Fatalf("%+v", res)
	}
}

func TestProtoGenerateError(t *testing.T) {
	e := newBufExecutor(t)
	res, err := e.Run(context.Background(), Files{"main.go": "package main\n\nfunc main() {}\n", "x.proto": "syntax = \"proto3\";\nmessage {\n"}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || !strings.Contains(res.BuildOutput, "x.proto") {
		t.Fatalf("%+v", res)
	}
}

func TestBufYAMLRestricted(t *testing.T) {
	defaults, err := protoDefaults.ReadFile("proto/buf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ok := "version: v2\nlint:\n  use: [MINIMAL]\n  except: [ENUM_ZERO_VALUE_SUFFIX]\nbreaking:\n  use: [WIRE_JSON]\n"
	canon, err := checkBufYAML(ok, string(defaults))
	if err != nil {
		t.Fatalf("допустимый buf.yaml отвергнут: %v", err)
	}
	if want := "version: v2\nlint:\n    use:\n        - MINIMAL\n    except:\n        - ENUM_ZERO_VALUE_SUFFIX\nbreaking:\n    use:\n        - WIRE_JSON\n"; string(canon) != want {
		t.Errorf("каноническая форма:\n%s\nwant:\n%s", canon, want)
	}
	for name, bad := range map[string]string{
		"plugins": "version: v2\nplugins:\n  - plugin: /bin/sh\n",
		// Второй документ buf сейчас не читает, но полагаться на это нельзя.
		"two docs": "version: v2\n---\nversion: v2\nplugins:\n  - plugin: /bin/sh\n",
		"deps":     "version: v2\ndeps:\n  - buf.build/acme/evil\n",
		"modules":  "version: v2\nmodules:\n  - path: /etc\n",
		"v1":       "version: v1\n",
	} {
		if _, err := checkBufYAML(bad, string(defaults)); err == nil {
			t.Errorf("%s: buf.yaml принят", name)
		}
	}
}

func TestBufCheck(t *testing.T) {
	e := newBufExecutor(t)
	ctx := context.Background()
	res, err := e.BufCheck(ctx, Files{"orders.proto": ordersProto}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Found || res.Failed {
		t.Fatalf("lint чистого контракта: %+v", res)
	}
	bad := strings.Replace(ordersProto, "GetOrderResponse) {", "Order) {", 1) + "\nmessage Order {}\n"
	res, err = e.BufCheck(ctx, Files{"orders.proto": bad}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Found || !strings.Contains(res.Output, "RPC_RESPONSE_STANDARD_NAME") {
		t.Fatalf("lint: %+v", res)
	}
	// Удалённое поле ломает совместимость с прежней версией.
	removed := strings.Replace(ordersProto, "  string customer = 2;\n", "", 1)
	res, err = e.BufCheck(ctx, Files{"orders.proto": removed}, Files{"orders.proto": ordersProto})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Found || !strings.Contains(res.Output, "FIELD_NO_DELETE") {
		t.Fatalf("breaking: %+v", res)
	}
	_, err = e.BufCheck(ctx, Files{"orders.proto": ordersProto, "buf.yaml": "version: v2\nplugins:\n  - plugin: sh\n"}, nil)
	var inputErr *InputError
	if !errors.As(err, &inputErr) {
		t.Fatalf("buf.yaml с plugins: err = %v", err)
	}
}

func TestProtoTooLarge(t *testing.T) {
	e := newBufExecutor(t)
	big := ordersProto + "\n// " + strings.Repeat("x", maxProtoBytes) + "\n"
	res, err := e.Run(context.Background(), Files{"main.go": "package main\n\nfunc main() {}\n", "orders.proto": big}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || !strings.Contains(res.BuildOutput, "КиБ") {
		t.Fatalf("%+v", res)
	}
	var inputErr *InputError
	if _, err := e.BufCheck(context.Background(), Files{"orders.proto": big}, nil); !errors.As(err, &inputErr) {
		t.Fatalf("BufCheck: %v, want InputError", err)
	}
}

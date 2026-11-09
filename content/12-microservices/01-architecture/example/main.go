// Итоговая система модуля в миниатюре: три «сервиса» в одном
// процессе на loopback. «Заказы» принимают REST-запрос,
// синхронно резервируют товар на «Складе» по gRPC и
// асинхронно публикуют событие в Kafka (в песочнице — `kfake`),
// которое читают «Уведомления». У каждого сервиса свои данные:
// никто не лезет в чужую map — только через контракт.

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

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const topic = "orders.events"

// «Склад» владеет остатками. Наружу — только `Reserve`
// из `warehouse.proto`; сгенерированный код лежит рядом.
type warehouse struct {
	UnimplementedWarehouseServiceServer
	mu    sync.Mutex
	stock map[string]int32
}

func (w *warehouse) Reserve(_ context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	need := map[string]int32{}
	for _, l := range req.GetLines() {
		if l.GetQty() <= 0 {
			return nil, status.Error(codes.InvalidArgument, "qty must be positive")
		}
		need[l.GetSku()] += l.GetQty()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for sku, n := range need {
		if w.stock[sku] < n {
			return nil, status.Errorf(codes.FailedPrecondition, "not enough %s", sku)
		}
	}
	for sku, n := range need {
		w.stock[sku] -= n
	}
	return &ReserveResponse{}, nil
}

// «Заказы» владеют заказами и знают соседей только как
// клиентов: gRPC-клиент «Склада» и продюсер Kafka.
type orders struct {
	stock  WarehouseServiceClient
	events *kgo.Client
	mu     sync.Mutex
	seq    int
	byID   map[string]string // id → покупатель: своя «база»
}

type orderRequest struct {
	Customer string `json:"customer"`
	Lines    []struct {
		SKU string `json:"sku"`
		Qty int32  `json:"qty"`
	} `json:"lines"`
}

func (o *orders) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req orderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Customer == "" || len(req.Lines) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	o.mu.Lock()
	o.seq++
	id := fmt.Sprintf("o-%d", o.seq)
	o.mu.Unlock()

	// Синхронный шаг: без резерва заказа нет, поэтому
	// ждём ответа «Склада» — но не дольше дедлайна.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	in := &ReserveRequest{OrderId: id}
	for _, l := range req.Lines {
		in.Lines = append(in.Lines, &Line{Sku: l.SKU, Qty: l.Qty})
	}
	if _, err := o.stock.Reserve(ctx, in); err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			http.Error(w, "out of stock", http.StatusConflict)
			return
		}
		http.Error(w, "warehouse unavailable", http.StatusBadGateway)
		return
	}
	o.mu.Lock()
	o.byID[id] = req.Customer
	o.mu.Unlock()

	// Асинхронный шаг: событие в лог Kafka, ключ — id
	// заказа. Кто и когда его прочитает, «Заказы» не знают.
	// Запись в map и в Kafka — двойная запись: упади мы
	// между ними, событие потеряется. Лечит outbox (урок 11).
	ev, _ := json.Marshal(map[string]string{"type": "order.created", "order_id": id, "customer": req.Customer})
	rec := &kgo.Record{Topic: topic, Key: []byte(id), Value: ev}
	if err := o.events.ProduceSync(ctx, rec).FirstErr(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"id":%q}`, id)
}

// «Уведомления» читают события с начала топика. Здесь
// чтение — синхронный вызов из `main`, чтобы вывод был
// детерминированным; настоящий консьюмер крутит цикл
// `PollFetches` в своей горутине, пока его не остановят.
func notifications(ctx context.Context, brokers []string, want int) error {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		return err
	}
	defer cl.Close()
	for got := 0; got < want; {
		fetches := cl.PollFetches(ctx)
		if err := fetches.Err0(); err != nil {
			return err
		}
		for r := range fetches.RecordsAll() {
			var ev map[string]string
			if err := json.Unmarshal(r.Value, &ev); err != nil {
				return err
			}
			fmt.Printf("уведомления: %s %s для %s\n", ev["type"], ev["order_id"], ev["customer"])
			got++
		}
	}
	return nil
}

func main() {
	ctx := context.Background()

	// Склад: gRPC-сервер на свободном порту loopback.
	wh := &warehouse{stock: map[string]int32{"tea": 5, "cup": 2}}
	gs := grpc.NewServer()
	RegisterWarehouseServiceServer(gs, wh)
	gl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go gs.Serve(gl)
	defer gs.GracefulStop()

	// Брокер: кластер Kafka в процессе, один топик.
	kc, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, topic))
	if err != nil {
		log.Fatal(err)
	}
	defer kc.Close()

	// Заказы: клиенты соседей собираются явно, в `main`.
	conn, err := grpc.NewClient(gl.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	producer, err := kgo.NewClient(kgo.SeedBrokers(kc.ListenAddrs()...))
	if err != nil {
		log.Fatal(err)
	}
	defer producer.Close()
	svc := &orders{stock: NewWarehouseServiceClient(conn), events: producer, byID: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", svc.create)
	hl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go hs.Serve(hl)
	defer hs.Shutdown(ctx)

	// Клиент снаружи: три заказа, второй не влезает в остаток.
	client := &http.Client{Timeout: 5 * time.Second}
	for _, body := range []string{
		`{"customer":"alice","lines":[{"sku":"tea","qty":2},{"sku":"cup","qty":1}]}`,
		`{"customer":"bob","lines":[{"sku":"cup","qty":5}]}`,
		`{"customer":"carol","lines":[{"sku":"tea","qty":3},{"sku":"cup","qty":1}]}`,
	} {
		resp, err := client.Post("http://"+hl.Addr().String()+"/orders", "application/json", strings.NewReader(body))
		if err != nil {
			log.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("POST /orders → %d %s\n", resp.StatusCode, bytes.TrimSpace(b))
	}
	wh.mu.Lock()
	fmt.Println("склад:", wh.stock) // fmt печатает map с отсортированными ключами
	wh.mu.Unlock()

	nctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := notifications(nctx, kc.ListenAddrs(), 2); err != nil {
		log.Fatal(err)
	}
}

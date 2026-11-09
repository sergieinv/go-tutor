package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNewRecordExactShape(t *testing.T) {
	r, err := NewRecord("t", OrderEvent{OrderID: "Z-9", Type: "refunded"})
	if err != nil {
		t.Fatalf("NewRecord = %v", err)
	}
	if want := `{"order_id":"Z-9","type":"refunded","amount":0}`; string(r.Value) != want {
		t.Errorf("Value = %s, want %s", r.Value, want)
	}
	want := []kgo.RecordHeader{
		{Key: "event-type", Value: []byte("refunded")},
		{Key: "content-type", Value: []byte("application/json")},
	}
	if !reflect.DeepEqual(r.Headers, want) {
		t.Errorf("Headers = %v, want ровно event-type и content-type по порядку", r.Headers)
	}
	if r.Partition != 0 {
		t.Errorf("Partition = %d: партицию выбирает партиционер клиента", r.Partition)
	}
}

func TestPublishPositionsMatchBroker(t *testing.T) {
	c, cl := newCluster(t, 4)
	// 30 событий шести заказов вперемешку; Amount — номер события.
	rng := rand.New(rand.NewPCG(1, 2))
	events := make([]OrderEvent, 30)
	for i := range events {
		events[i] = OrderEvent{OrderID: fmt.Sprintf("O-%d", rng.IntN(6)), Type: fmt.Sprintf("e%d", i), Amount: int64(i)}
	}
	pos, err := Publish(testCtx(t), cl, testTopic, events)
	if err != nil {
		t.Fatalf("Publish = %v", err)
	}
	if len(pos) != len(events) {
		t.Fatalf("позиций %d, want %d", len(pos), len(events))
	}
	recs := readAll(t, c, len(events))
	for _, r := range recs {
		var e OrderEvent
		if err := json.Unmarshal(r.Value, &e); err != nil {
			t.Fatal(err)
		}
		want := Position{Partition: r.Partition, Offset: r.Offset}
		if pos[e.Amount] != want {
			t.Errorf("позиция события %d = %+v, у брокера %+v — позиции должны идти в порядке events", e.Amount, pos[e.Amount], want)
		}
	}
	// История каждого заказа — ровно в порядке отправки.
	want := map[string][]string{}
	for _, e := range events {
		want[e.OrderID] = append(want[e.OrderID], e.Type)
	}
	rng.Shuffle(len(recs), func(i, j int) { recs[i], recs[j] = recs[j], recs[i] })
	got, err := Histories(recs)
	if err != nil {
		t.Fatalf("Histories = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Histories = %v, want %v", got, want)
	}
}

func TestPublishValidatesBeforeSending(t *testing.T) {
	c, cl := newCluster(t, 3)
	events := []OrderEvent{{"A-1", "created", 1}, {"B-2", "created", 2}, {"", "created", 3}}
	pos, err := Publish(testCtx(t), cl, testTopic, events)
	if !errors.Is(err, ErrNoOrderID) || pos != nil {
		t.Fatalf("Publish = %v, %v; want nil, ErrNoOrderID", pos, err)
	}
	adm := kadm.NewClient(cl)
	ends, err := adm.ListEndOffsets(testCtx(t), testTopic)
	if err != nil {
		t.Fatal(err)
	}
	ends.Each(func(o kadm.ListedOffset) {
		if o.Offset != 0 {
			t.Errorf("партиция %d: офсет конца %d — при ошибке проверки ничего не должно уйти", o.Partition, o.Offset)
		}
	})
	_ = c
}

func TestPublishEmpty(t *testing.T) {
	// nil-клиент: к брокеру обращаться незачем.
	pos, err := Publish(context.Background(), nil, testTopic, nil)
	if err != nil || pos == nil || len(pos) != 0 {
		t.Fatalf("Publish(nil events) = %v, %v; want пустой срез, nil", pos, err)
	}
}

func TestPublishSendError(t *testing.T) {
	_, cl := newCluster(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pos, err := Publish(ctx, cl, testTopic, []OrderEvent{{"A-1", "created", 1}})
	if !errors.Is(err, context.Canceled) || pos != nil {
		t.Fatalf("Publish с отменённым ctx = %v, %v; want nil и ошибку отправки (context.Canceled)", pos, err)
	}
}

func TestHistoriesUnorderedInput(t *testing.T) {
	ev := func(key string, p int32, off int64, typ string) *kgo.Record {
		v, _ := json.Marshal(OrderEvent{OrderID: key, Type: typ})
		return &kgo.Record{Key: []byte(key), Partition: p, Offset: off, Value: v}
	}
	recs := []*kgo.Record{
		ev("B-2", 1, 1, "paid"), ev("A-1", 0, 2, "shipped"), ev("C-3", 1, 2, "created"),
		ev("A-1", 0, 0, "created"), ev("B-2", 1, 0, "created"), ev("A-1", 0, 1, "paid"),
	}
	got, err := Histories(recs)
	if err != nil {
		t.Fatalf("Histories = %v", err)
	}
	want := map[string][]string{
		"A-1": {"created", "paid", "shipped"},
		"B-2": {"created", "paid"},
		"C-3": {"created"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Histories = %v, want %v", got, want)
	}
}

func TestHistoriesEmpty(t *testing.T) {
	got, err := Histories(nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Histories(nil) = %v, %v; want пустую не-nil карту", got, err)
	}
}

func TestHistoriesBadInput(t *testing.T) {
	ok := []byte(`{"order_id":"A-1","type":"created"}`)
	got, err := Histories([]*kgo.Record{{Key: nil, Value: ok}})
	if !errors.Is(err, ErrNoOrderID) || got != nil {
		t.Errorf("запись без ключа: %v, %v; want nil, ErrNoOrderID", got, err)
	}
	got, err = Histories([]*kgo.Record{{Key: []byte("A-1"), Value: ok}, {Key: []byte("A-1"), Offset: 1, Value: []byte("{oops")}})
	if err == nil || got != nil || errors.Is(err, ErrSplitKey) {
		t.Errorf("битый JSON: %v, %v; want nil и ошибку разбора", got, err)
	}
	got, err = Histories([]*kgo.Record{
		{Key: []byte("B-2"), Partition: 2, Offset: 5, Value: ok},
		{Key: []byte("A-1"), Partition: 0, Offset: 0, Value: ok},
		{Key: []byte("B-2"), Partition: 1, Offset: 7, Value: ok},
	})
	if !errors.Is(err, ErrSplitKey) || got != nil {
		t.Errorf("ключ в двух партициях: %v, %v; want nil, ErrSplitKey", got, err)
	}
}

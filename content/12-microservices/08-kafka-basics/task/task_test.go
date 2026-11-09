package task

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

const testTopic = "orders.events"

// newCluster поднимает Kafka в процессе с топиком из partitions партиций
// и возвращает клиент с настройками по умолчанию.
func newCluster(t *testing.T, partitions int32) (*kfake.Cluster, *kgo.Client) {
	t.Helper()
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(partitions, testTopic))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	return c, cl
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// readAll читает из топика ровно n записей с начала.
func readAll(t *testing.T, c *kfake.Cluster, n int) []*kgo.Record {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.ConsumeTopics(testTopic), kgo.FetchMaxWait(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx := testCtx(t)
	var out []*kgo.Record
	for len(out) < n {
		f := cl.PollFetches(ctx)
		if err := f.Err(); err != nil {
			t.Fatalf("чтение: %v (прочитано %d из %d)", err, len(out), n)
		}
		out = append(out, f.Records()...)
	}
	return out
}

func header(r *kgo.Record, key string) (string, bool) {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value), true
		}
	}
	return "", false
}

func TestNewRecord(t *testing.T) {
	r, err := NewRecord("orders.events", OrderEvent{OrderID: "A-1", Type: "created", Amount: 300})
	if err != nil {
		t.Fatalf("NewRecord = %v", err)
	}
	if r.Topic != "orders.events" {
		t.Errorf("Topic = %q", r.Topic)
	}
	if string(r.Key) != "A-1" {
		t.Errorf("Key = %q, want A-1", r.Key)
	}
	if want := `{"order_id":"A-1","type":"created","amount":300}`; string(r.Value) != want {
		t.Errorf("Value = %s, want %s", r.Value, want)
	}
	if v, _ := header(r, HeaderEventType); v != "created" {
		t.Errorf("заголовок event-type = %q, want created", v)
	}
	if v, _ := header(r, HeaderContentType); v != ContentTypeJSON {
		t.Errorf("заголовок content-type = %q", v)
	}
}

func TestNewRecordNoOrderID(t *testing.T) {
	r, err := NewRecord("orders.events", OrderEvent{Type: "created"})
	if !errors.Is(err, ErrNoOrderID) || r != nil {
		t.Fatalf("NewRecord без OrderID = %v, %v; want nil, ErrNoOrderID", r, err)
	}
}

func TestPublishSameOrderSamePartition(t *testing.T) {
	c, cl := newCluster(t, 3)
	events := []OrderEvent{
		{"A-1", "created", 300}, {"B-2", "created", 120}, {"A-1", "paid", 300},
		{"A-1", "shipped", 0}, {"B-2", "cancelled", 0},
	}
	pos, err := Publish(testCtx(t), cl, testTopic, events)
	if err != nil {
		t.Fatalf("Publish = %v", err)
	}
	if len(pos) != len(events) {
		t.Fatalf("позиций %d, want %d", len(pos), len(events))
	}
	if pos[0].Partition != pos[2].Partition || pos[0].Partition != pos[3].Partition {
		t.Errorf("события A-1 в разных партициях: %v", pos)
	}
	if !(pos[0].Offset < pos[2].Offset && pos[2].Offset < pos[3].Offset) {
		t.Errorf("офсеты A-1 не растут: %v", pos)
	}

	got, err := Histories(readAll(t, c, len(events)))
	if err != nil {
		t.Fatalf("Histories = %v", err)
	}
	want := map[string][]string{"A-1": {"created", "paid", "shipped"}, "B-2": {"created", "cancelled"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Histories = %v, want %v", got, want)
	}
}

func TestHistoriesSplitKey(t *testing.T) {
	recs := []*kgo.Record{
		{Key: []byte("A-1"), Partition: 0, Offset: 0, Value: []byte(`{"order_id":"A-1","type":"created"}`)},
		{Key: []byte("A-1"), Partition: 1, Offset: 0, Value: []byte(`{"order_id":"A-1","type":"paid"}`)},
	}
	if _, err := Histories(recs); !errors.Is(err, ErrSplitKey) {
		t.Fatalf("Histories = %v, want ErrSplitKey", err)
	}
}

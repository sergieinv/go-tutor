package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	testTopic = "orders.events"
	testGroup = "notifications"
)

var errBoom = errors.New("smtp недоступен")

// newCluster поднимает Kafka в процессе и пишет в топик values
// (ключ — keys[i % len(keys)]).
func newCluster(t *testing.T, partitions int32, keys []string, values ...string) *kfake.Cluster {
	t.Helper()
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(partitions, testTopic))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	produce(t, c, keys, values...)
	return c
}

func produce(t *testing.T, c *kfake.Cluster, keys []string, values ...string) {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	var recs []*kgo.Record
	for i, v := range values {
		recs = append(recs, &kgo.Record{Topic: testTopic, Key: []byte(keys[i%len(keys)]), Value: []byte(v)})
	}
	if err := cl.ProduceSync(context.Background(), recs...).FirstErr(); err != nil {
		t.Fatal(err)
	}
}

// newConsumer — участник группы с ручным коммитом.
func newConsumer(t *testing.T, c *kfake.Cluster) *kgo.Client {
	t.Helper()
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(c.ListenAddrs()...),
		kgo.ConsumerGroup(testGroup),
		kgo.ConsumeTopics(testTopic),
		kgo.DisableAutoCommit(),
		kgo.FetchMaxWait(200*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

// committed — закоммиченные офсеты группы по партициям.
func committed(t *testing.T, c *kfake.Cluster) map[int32]int64 {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	out := map[int32]int64{}
	offs, err := kadm.NewClient(cl).FetchOffsets(context.Background(), testGroup)
	if errors.Is(err, kerr.GroupIDNotFound) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	offs.Each(func(o kadm.OffsetResponse) {
		if o.At >= 0 {
			out[o.Partition] = o.At
		}
	})
	return out
}

// recorder запоминает обработанные записи; fail решает, упасть ли.
type recorder struct {
	mu   sync.Mutex
	got  []*kgo.Record
	fail func(r *kgo.Record) error
	// after вызывается после успешной обработки с числом обработанных.
	after func(n int)
}

func (rc *recorder) handle(ctx context.Context, r *kgo.Record) error {
	if rc.fail != nil {
		if err := rc.fail(r); err != nil {
			return err
		}
	}
	rc.mu.Lock()
	rc.got = append(rc.got, r)
	n := len(rc.got)
	rc.mu.Unlock()
	if rc.after != nil {
		rc.after(n)
	}
	return nil
}

func (rc *recorder) values() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	var out []string
	for _, r := range rc.got {
		out = append(out, string(r.Value))
	}
	return out
}

func waitCommitted(t *testing.T, c *kfake.Cluster, want map[int32]int64) {
	t.Helper()
	var got map[int32]int64
	for range 100 {
		if got = committed(t, c); fmt.Sprint(got) == fmt.Sprint(want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("закоммичено %v, want %v", got, want)
}

func TestFailureIsRedelivered(t *testing.T) {
	c := newCluster(t, 1, []string{"A-1"}, "0", "1", "2", "3", "4")

	// Запуск 1: запись "2" не обрабатывается.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rc := &recorder{fail: func(r *kgo.Record) error {
		if string(r.Value) == "2" {
			return errBoom
		}
		return nil
	}}
	cl := newConsumer(t, c)
	err := Run(ctx, cl, rc.handle)
	cl.Close()
	if !errors.Is(err, errBoom) {
		t.Fatalf("Run = %v, want ошибку обработчика", err)
	}
	if got := rc.values(); !slices.Equal(got, []string{"0", "1"}) {
		t.Fatalf("обработано %v, want [0 1]", got)
	}
	if got := committed(t, c); got[0] != 2 {
		t.Fatalf("закоммичено %v, want p0=2", got)
	}

	// Запуск 2: упавшая запись приходит снова.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	rc2 := &recorder{after: func(n int) {
		if n == 3 {
			cancel2()
		}
	}}
	cl2 := newConsumer(t, c)
	defer cl2.Close()
	if err := Run(ctx2, cl2, rc2.handle); err != nil {
		t.Fatalf("Run после отмены ctx = %v, want nil", err)
	}
	if got := rc2.values(); !slices.Equal(got, []string{"2", "3", "4"}) {
		t.Fatalf("после перезапуска обработано %v, want [2 3 4]", got)
	}
	if got := committed(t, c); got[0] != 5 {
		t.Fatalf("закоммичено %v, want p0=5", got)
	}
}

func TestCommitBeforeNextPoll(t *testing.T) {
	c := newCluster(t, 1, []string{"A-1"}, "0", "1", "2")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cl := newConsumer(t, c)
	defer cl.Close()
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, cl, (&recorder{}).handle) }()

	// Run ждёт новых записей, а обработанное уже закоммичено.
	waitCommitted(t, c, map[int32]int64{0: 3})
	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Run = %v, want nil после отмены ctx", err)
	}
}

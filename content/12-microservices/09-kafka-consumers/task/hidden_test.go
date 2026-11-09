package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

type pos struct {
	p int32
	o int64
}

func positions(recs []*kgo.Record) []pos {
	out := make([]pos, len(recs))
	for i, r := range recs {
		out[i] = pos{r.Partition, r.Offset}
	}
	return out
}

func checkPartitionOrder(t *testing.T, run string, recs []*kgo.Record) {
	t.Helper()
	last := map[int32]int64{}
	for _, r := range recs {
		if o, ok := last[r.Partition]; ok && r.Offset <= o {
			t.Errorf("%s: в партиции %d офсет %d после %d — порядок внутри партиции нарушен", run, r.Partition, r.Offset, o)
		}
		last[r.Partition] = r.Offset
	}
}

func TestAtLeastOnceAcrossPartitions(t *testing.T) {
	keys := []string{"A-1", "B-2", "C-3", "D-4", "E-5"}
	var values []string
	for i := range 15 {
		values = append(values, strconv.Itoa(i))
	}
	c := newCluster(t, 3, keys, values...)

	var failed *kgo.Record
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rc := &recorder{fail: func(r *kgo.Record) error {
		if string(r.Value) == "7" {
			failed = r
			return errBoom
		}
		return nil
	}}
	cl := newConsumer(t, c)
	err := Run(ctx, cl, rc.handle)
	cl.Close()
	if !errors.Is(err, errBoom) || failed == nil {
		t.Fatalf("Run = %v, want ошибку обработчика на записи 7", err)
	}
	run1 := rc.got
	checkPartitionOrder(t, "запуск 1", run1)
	for _, r := range run1 {
		if r.Partition == failed.Partition && r.Offset > failed.Offset {
			t.Errorf("запуск 1: запись p%d@%d обработана после упавшей p%d@%d", r.Partition, r.Offset, failed.Partition, failed.Offset)
		}
	}

	left := len(values) - len(run1)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	rc2 := &recorder{after: func(n int) {
		if n == left {
			cancel2()
		}
	}}
	cl2 := newConsumer(t, c)
	defer cl2.Close()
	if err := Run(ctx2, cl2, rc2.handle); err != nil {
		t.Fatalf("Run (запуск 2) = %v", err)
	}
	run2 := rc2.got
	checkPartitionOrder(t, "запуск 2", run2)

	// Каждая запись обработана ровно один раз за два запуска,
	// упавшая — во втором.
	seen := map[pos]string{}
	for _, p := range positions(run1) {
		seen[p] = "запуск 1"
	}
	for _, p := range positions(run2) {
		if prev, ok := seen[p]; ok {
			t.Errorf("p%d@%d обработана повторно (уже в %s) — успешно обработанное должно быть закоммичено", p.p, p.o, prev)
		}
		seen[p] = "запуск 2"
	}
	if len(seen) != len(values) {
		t.Errorf("за два запуска обработано %d разных записей, want %d", len(seen), len(values))
	}
	if seen[pos{failed.Partition, failed.Offset}] != "запуск 2" {
		t.Errorf("упавшая запись не пришла после перезапуска")
	}

	want := map[int32]int64{}
	for _, r := range append(slices.Clone(run1), run2...) {
		want[r.Partition]++
	}
	waitCommitted(t, c, want)
}

func TestCancelInHandlerCommitsProcessed(t *testing.T) {
	c := newCluster(t, 1, []string{"A-1"}, "0", "1", "2", "3", "4")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Обработчик получил SIGTERM во время записи "1", но доделал её.
	rc := &recorder{after: func(n int) {
		if n == 2 {
			cancel()
		}
	}}
	cl := newConsumer(t, c)
	err := Run(ctx, cl, rc.handle)
	cl.Close()
	if err != nil {
		t.Fatalf("Run после отмены ctx = %v, want nil", err)
	}
	if got := rc.values(); !slices.Equal(got, []string{"0", "1"}) {
		t.Fatalf("обработано %v, want [0 1] — после отмены ctx новые записи не обрабатываются", got)
	}
	if got := committed(t, c); got[0] != 2 {
		t.Fatalf("закоммичено %v, want p0=2 — коммит должен пройти и на отменённом ctx (context.WithoutCancel)", got)
	}
}

func TestFailOnFirstRecordCommitsNothing(t *testing.T) {
	c := newCluster(t, 1, []string{"A-1"}, "0", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	calls := 0
	cl := newConsumer(t, c)
	err := Run(ctx, cl, func(context.Context, *kgo.Record) error {
		calls++
		return errBoom
	})
	cl.Close()
	if !errors.Is(err, errBoom) {
		t.Fatalf("Run = %v, want ошибку обработчика", err)
	}
	if calls != 1 {
		t.Errorf("обработчик вызван %d раз, want 1 — после ошибки записи не обрабатываются", calls)
	}
	if got := committed(t, c); len(got) != 0 {
		t.Errorf("закоммичено %v, want ничего", got)
	}
}

func TestClosedClientStopsRun(t *testing.T) {
	c := newCluster(t, 2, []string{"A-1", "B-2"}, "0", "1", "2", "3")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cl := newConsumer(t, c)
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, cl, (&recorder{}).handle) }()
	waitCommitted(t, c, committedAll(t, c))
	cl.Close()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run после Close = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run не вернулся после закрытия клиента")
	}
}

// committedAll — ожидаемые офсеты, когда всё прочитано: число записей
// в каждой партиции.
func committedAll(t *testing.T, c *kfake.Cluster) map[int32]int64 {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.ConsumeTopics(testTopic), kgo.FetchMaxWait(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out := map[int32]int64{}
	for n := 0; n < 4; {
		f := cl.PollFetches(ctx)
		if err := f.Err(); err != nil {
			t.Fatal(fmt.Errorf("чтение: %w", err))
		}
		f.EachRecord(func(r *kgo.Record) { out[r.Partition]++; n++ })
	}
	return out
}

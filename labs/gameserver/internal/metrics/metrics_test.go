package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"gotutor/labs/gameserver/internal/world"
)

// TestHotPathNoAllocs — метрики, которые зовёт тик, не аллоцируют:
// серии причин получены заранее.
func TestHotPathNoAllocs(t *testing.T) {
	m := New(prometheus.NewRegistry())
	allocs := testing.AllocsPerRun(1000, func() {
		m.Disconnect(world.ReasonSlowConsumer)
		m.RejectedInput("too_fast")
		m.JoinRejected("full")
		m.TickDone(time.Millisecond)
		m.Players(world.KindWeb, 3)
		m.BytesOut("minecraft", 10)
	})
	if allocs != 0 {
		t.Fatalf("hot path allocates %.1f times", allocs)
	}
	if v := testutil.ToFloat64(m.disconnects.WithLabelValues(world.ReasonSlowConsumer)); v != 1001 {
		t.Fatalf("slow_consumer = %v", v)
	}
}

func TestSnapshotSeriesPresent(t *testing.T) {
	m := New(prometheus.NewRegistry())
	for _, r := range []string{"ok", "skipped", "error"} {
		if v := testutil.ToFloat64(m.snapshots.WithLabelValues(r)); v != 0 {
			t.Fatalf("%s = %v", r, v)
		}
	}
	if n := testutil.CollectAndCount(m.snapshots); n != 3 {
		t.Fatalf("snapshot series = %d, want 3 from start", n)
	}
	m.Snapshot("skipped", 0)
	if v := testutil.ToFloat64(m.snapshots.WithLabelValues("skipped")); v != 1 {
		t.Fatalf("skipped = %v", v)
	}
}

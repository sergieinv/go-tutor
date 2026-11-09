package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotutor/labs/notifications/internal/notify"
)

// Контрактные тесты хранилища: каждый — на своём файле в t.TempDir(),
// поэтому они независимы и могут идти параллельно.

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notifications.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s, path
}

var at = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func note(eventID string, orderID int64, kind notify.Kind) notify.Notification {
	return notify.Notification{
		EventID: eventID, OrderID: orderID, CustomerID: 5, Kind: kind,
		Text: "Заказ оформлен", CreatedAt: at,
	}
}

func notifications(t *testing.T, s *Store, orderID int64) []notify.Notification {
	t.Helper()
	got, err := s.Notifications(t.Context(), orderID)
	if err != nil {
		t.Fatalf("Notifications: %v", err)
	}
	return got
}

func TestSaveOnce(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t)
	n := note("e1", 42, notify.KindOrderCreated)

	saved, err := s.SaveOnce(t.Context(), n)
	if err != nil || !saved {
		t.Fatalf("first SaveOnce = %v, %v; want true", saved, err)
	}
	if got := notifications(t, s, 42); len(got) != 1 || got[0] != n {
		t.Fatalf("Notifications = %+v, want [%+v]", got, n)
	}
}

func TestSaveOnceDeduplicates(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t)
	n := note("e1", 42, notify.KindOrderCreated)
	if _, err := s.SaveOnce(t.Context(), n); err != nil {
		t.Fatal(err)
	}
	// Повтор с тем же event_id, даже с другим содержимым, — не сохраняется.
	dup := n
	dup.Text = "другой текст"
	saved, err := s.SaveOnce(t.Context(), dup)
	if err != nil || saved {
		t.Fatalf("duplicate SaveOnce = %v, %v; want false, nil", saved, err)
	}
	if got := notifications(t, s, 42); len(got) != 1 || got[0].Text != n.Text {
		t.Fatalf("Notifications = %+v, want only the first", got)
	}
}

func TestSaveOnceConcurrentDuplicates(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t)
	const workers = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		saved int
	)
	for range workers {
		wg.Go(func() {
			ok, err := s.SaveOnce(t.Context(), note("e1", 42, notify.KindOrderCreated))
			if err != nil {
				t.Errorf("SaveOnce: %v", err)
				return
			}
			if ok {
				mu.Lock()
				saved++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if saved != 1 || len(notifications(t, s, 42)) != 1 {
		t.Errorf("saved %d times, want exactly 1", saved)
	}
}

func TestNotificationsOrder(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t)
	for _, n := range []notify.Notification{
		note("e1", 42, notify.KindOrderCreated),
		note("e2", 7, notify.KindOrderCreated),
		note("e3", 42, notify.KindOrderCancelled),
	} {
		if _, err := s.SaveOnce(t.Context(), n); err != nil {
			t.Fatal(err)
		}
	}
	got := notifications(t, s, 42)
	if len(got) != 2 || got[0].EventID != "e1" || got[1].EventID != "e3" {
		t.Errorf("Notifications(42) = %+v, want e1, e3", got)
	}
	if got := notifications(t, s, 1); len(got) != 0 {
		t.Errorf("Notifications(1) = %+v, want none", got)
	}
}

func TestSaveOnceRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t)
	// Недопустимый kind нарушает CHECK во второй вставке: отметка о
	// событии из первой вставки должна откатиться вместе с ней.
	bad := note("e1", 42, notify.Kind("sms"))
	if _, err := s.SaveOnce(t.Context(), bad); err == nil {
		t.Fatal("SaveOnce with invalid kind: want error")
	}
	saved, err := s.SaveOnce(t.Context(), note("e1", 42, notify.KindOrderCreated))
	if err != nil || !saved {
		t.Fatalf("SaveOnce after rollback = %v, %v; want true (event not marked processed)", saved, err)
	}
}

func TestSaveOnceCanceledContext(t *testing.T) {
	t.Parallel()
	s, _ := openTemp(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.SaveOnce(ctx, note("e1", 42, notify.KindOrderCreated)); err == nil {
		t.Fatal("SaveOnce with canceled context: want error")
	}
}

func TestDataSurvivesReopenAndMigrateIsIdempotent(t *testing.T) {
	t.Parallel()
	s, path := openTemp(t)
	if _, err := s.SaveOnce(t.Context(), note("e1", 42, notify.KindOrderCreated)); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := s2.Migrate(t.Context()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if saved, err := s2.SaveOnce(t.Context(), note("e1", 42, notify.KindOrderCreated)); err != nil || saved {
		t.Fatalf("SaveOnce after reopen = %v, %v; want duplicate", saved, err)
	}
	if err := s2.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestOpenRejectsNonFilePaths(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"", ":memory:", "file.db?mode=ro", "a#b"} {
		if s, err := Open(t.Context(), p); err == nil {
			s.Close()
			t.Errorf("Open(%q): want error", p)
		}
	}
}

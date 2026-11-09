package sqlite_test

import (
	"path/filepath"
	"testing"

	"gotutor/labs/orders/internal/store/sqlite"
	"gotutor/labs/orders/internal/storetest"
)

// SQLite не нужен сервер: контрактный набор против него идёт всегда.
func TestStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Store {
		// Файл во временном каталоге теста: Go удалит его сам.
		s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "orders.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(s.Close)
		if err := s.Migrate(t.Context()); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		return s
	})
}

func TestOpenRejectsMemory(t *testing.T) {
	for _, path := range []string{"", ":memory:", "file.db?mode=memory"} {
		if s, err := sqlite.Open(t.Context(), path); err == nil {
			s.Close()
			t.Errorf("Open(%q) succeeded, want error", path)
		}
	}
}

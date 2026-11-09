package memstore_test

import (
	"testing"

	"gotutor/labs/warehouse/internal/stock"
	"gotutor/labs/warehouse/internal/store/memstore"
	"gotutor/labs/warehouse/internal/storetest"
)

// TestStore — контрактный набор на хранилище в памяти: запускается всегда.
func TestStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T, levels map[int64]int32) stock.Store {
		return memstore.New(levels)
	})
}

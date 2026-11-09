package save

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotutor/labs/gameserver/internal/world"
)

type obs struct{ results map[string]int }

func (o *obs) Snapshot(r string, _ time.Duration) { o.results[r]++ }

func TestSaveLoadAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.json")
	o := &obs{results: map[string]int{}}
	s := New(path, slog.New(slog.DiscardHandler), o)
	if snap, err := s.Load(); err != nil || snap != nil {
		t.Fatalf("missing file: %v %v", snap, err)
	}
	want := &world.Snapshot{Tick: 42,
		Players: []world.PlayerState{{Name: "alice", X: 1, Y: -60, Z: 2}},
		Blocks:  []world.BlockEdit{{X: 1, Y: -60, Z: 2, State: world.Stone}}}
	go s.Run()
	if !s.Offer(want) {
		t.Fatal("offer rejected")
	}
	s.Close()
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Tick != 42 || len(got.Players) != 1 || got.Blocks[0].State != world.Stone {
		t.Fatalf("got %+v", got)
	}
	// Временных файлов не осталось.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries", len(entries))
	}
	if o.results["ok"] != 1 {
		t.Fatalf("results %v", o.results)
	}
}

func TestOfferDoesNotBlock(t *testing.T) {
	o := &obs{results: map[string]int{}}
	s := New(filepath.Join(t.TempDir(), "w.json"), slog.New(slog.DiscardHandler), o)
	// Run не запущен: первый снапшот принят и не записан — второй сразу
	// пропускается (а не встаёт в очередь), Busy это сообщает заранее.
	if s.Busy() || !s.Offer(&world.Snapshot{}) || !s.Busy() || s.Offer(&world.Snapshot{}) {
		t.Fatal("second offer must be skipped, not queued or blocked")
	}
	if o.results["skipped"] != 1 {
		t.Fatalf("results %v", o.results)
	}
}

func TestCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := New(path, slog.New(slog.DiscardHandler), &obs{results: map[string]int{}}).Load(); err == nil {
		t.Fatal("corrupt file must be an error, not an empty world")
	}
}

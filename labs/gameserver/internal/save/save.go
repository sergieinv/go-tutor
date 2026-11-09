// Пакет save — асинхронное сохранение мира снапшотами.
//
// Тик не пишет на диск: он делает копию состояния (world.Snapshot) и
// отдаёт её сюда неблокирующим Offer. Отдельная горутина кодирует снапшот
// и пишет файл атомарно: во временный файл рядом, fsync, rename поверх
// старого, fsync каталога. Упади процесс посреди записи — на диске
// останется прошлый целый снапшот, а не половина нового.
package save

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"gotutor/labs/gameserver/internal/world"
)

// formatVersion — версия формата файла. id состояний блоков — из
// Minecraft 1.21.1: при смене версии игры их нужно перекодировать.
const formatVersion = 1

type file struct {
	Format    int    `json:"format"`
	Minecraft string `json:"minecraft"`
	SavedAt   string `json:"saved_at"`
	*world.Snapshot
}

// Observer — метрики сохранения.
type Observer interface {
	Snapshot(result string, d time.Duration)
}

// Store пишет снапшоты в файл path.
type Store struct {
	path string
	log  *slog.Logger
	obs  Observer
	ch   chan *world.Snapshot
	done chan struct{}
	// busy — снапшот принят и ещё не записан (в канале или пишется).
	busy atomic.Bool
}

// New — path — файл снапшота (каталог должен существовать).
func New(path string, log *slog.Logger, obs Observer) *Store {
	return &Store{path: path, log: log, obs: obs, ch: make(chan *world.Snapshot, 1), done: make(chan struct{})}
}

// Load читает снапшот; (nil, nil), если файла ещё нет.
func (s *Store) Load() (*world.Snapshot, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f := file{Snapshot: &world.Snapshot{}}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode %s: %w", s.path, err)
	}
	if f.Format != formatVersion {
		return nil, fmt.Errorf("%s: unsupported format %d", s.path, f.Format)
	}
	return f.Snapshot, nil
}

// Busy — прошлый снапшот ещё не записан. Тик спрашивает до того, как
// копировать состояние: незачем делать копию, которую некуда отдать.
func (s *Store) Busy() bool { return s.busy.Load() }

// Skip учитывает снапшот, который тик не стал делать, потому что Busy.
func (s *Store) Skip() { s.obs.Snapshot("skipped", 0) }

// Offer отдаёт снапшот на запись, не блокируясь. false — прошлый ещё не
// записан; тик не ждёт диск, этот снапшот пропускается.
func (s *Store) Offer(snap *world.Snapshot) bool {
	if !s.busy.CompareAndSwap(false, true) {
		s.obs.Snapshot("skipped", 0)
		return false
	}
	s.ch <- snap // канал на 1 место пуст: busy был false
	return true
}

// Run пишет снапшоты, пока не вызван Close.
func (s *Store) Run() {
	defer close(s.done)
	for snap := range s.ch {
		if err := s.Save(snap); err != nil {
			s.log.Error("snapshot failed", "err", err)
		}
		s.busy.Store(false)
	}
}

// Close дожидается записи принятого снапшота и останавливает Run. После
// Close вызывать Offer нельзя (мир к этому моменту уже остановлен).
func (s *Store) Close() {
	close(s.ch)
	<-s.done
}

// Save пишет снапшот синхронно (финальное сохранение при остановке).
func (s *Store) Save(snap *world.Snapshot) error {
	start := time.Now()
	err := s.write(snap)
	if err != nil {
		s.obs.Snapshot("error", 0)
		return err
	}
	d := time.Since(start)
	s.obs.Snapshot("ok", d)
	s.log.Info("world saved", "tick", snap.Tick, "players", len(snap.Players), "blocks", len(snap.Blocks), "duration", d)
	return nil
}

func (s *Store) write(snap *world.Snapshot) error {
	snap.Sort() // снапшот принадлежит нам: тик отдал копию
	data, err := json.Marshal(file{
		Format: formatVersion, Minecraft: "1.21.1",
		SavedAt: time.Now().UTC().Format(time.RFC3339), Snapshot: snap,
	})
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".world-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // после успешного rename файла уже нет — no-op
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // данные на диске до rename
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	// fsync каталога фиксирует саму запись о переименовании.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

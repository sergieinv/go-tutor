package task

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

// Горутина записи читает снапшот одновременно с тем, как тик
// переписывает те же блоки. Без copy-on-write это гонка (её поймает
// -race) и «рваный» снапшот.
func TestHiddenConcurrentReadWhileTicking(t *testing.T) {
	proceed := make(chan struct{})
	result := make(chan []uint16, 1)
	w := NewWorld(func(s *Snapshot) error {
		<-proceed
		var got []uint16
		for range 200 {
			for x := range 32 {
				got = append(got, s.Block(x, -x))
			}
		}
		result <- got
		return nil
	})
	for x := range 32 {
		w.SetBlock(x, -x, uint16(x+1))
	}
	w.Tick()
	if !w.StartSave() {
		t.Fatal("StartSave = false")
	}
	close(proceed)
	within(t, "тик во время сохранения", func() {
		for i := range 200 {
			w.Tick()
			for x := range 32 {
				w.SetBlock(x, -x, uint16(1000+i))
			}
		}
	})
	var got []uint16
	select {
	case got = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("save не закончила чтение")
	}
	for i, b := range got {
		if want := uint16(i%32 + 1); b != want {
			t.Fatalf("снапшот прочитал %d в точке %d, want %d — снапшот меняется после StartSave", b, i%32, want)
		}
	}
	if err := w.Wait(); err != nil {
		t.Fatalf("Wait = %v", err)
	}
	if got := w.Block(5, -5); got != 1199 {
		t.Errorf("мир: Block(5, -5) = %d, want 1199", got)
	}
}

func TestHiddenSaveCalledOncePerStart(t *testing.T) {
	s := newSlowSave()
	w := NewWorld(s.save)
	w.StartSave()
	s.next(t)
	for range 5 {
		if w.StartSave() {
			t.Fatal("StartSave во время записи = true")
		}
	}
	close(s.release)
	w.Wait()
	select {
	case <-s.started:
		t.Fatal("save вызвана лишний раз: отклонённый StartSave не должен ничего запускать")
	default:
	}
}

func TestHiddenWaitReturnsLastError(t *testing.T) {
	boom := errors.New("диск полон")
	var errs = []error{boom, nil}
	calls := 0
	w := NewWorld(func(*Snapshot) error {
		err := errs[calls]
		calls++
		return err
	})
	if err := w.Wait(); err != nil {
		t.Fatalf("Wait без сохранений = %v, want nil", err)
	}
	w.StartSave()
	if err := w.Wait(); !errors.Is(err, boom) {
		t.Fatalf("Wait = %v, want %v", err, boom)
	}
	if err := w.Wait(); !errors.Is(err, boom) {
		t.Fatalf("повторный Wait = %v, want %v — ошибка последнего сохранения", err, boom)
	}
	if !w.StartSave() {
		t.Fatal("StartSave после упавшего сохранения = false, want true")
	}
	if err := w.Wait(); err != nil {
		t.Fatalf("Wait после удачного сохранения = %v, want nil", err)
	}
}

// Счёт копий: StartSave ничего не копирует, первая запись в чанк
// после снапшота копирует его, следующие — на месте; новые чанки
// не копируются; отклонённый StartSave ничего не меняет.
func TestHiddenCopyOnWriteCount(t *testing.T) {
	s := newSlowSave()
	w := NewWorld(s.save)
	w.SetBlock(0, 0, 1)   // чанк (0,0)
	w.SetBlock(20, 0, 1)  // чанк (1,0)
	w.SetBlock(-1, -1, 1) // чанк (-1,-1)
	if got := w.Copies(); got != 0 {
		t.Fatalf("Copies до снапшота = %d, want 0", got)
	}
	w.StartSave()
	snap := s.next(t)
	if got := w.Copies(); got != 0 {
		t.Fatalf("Copies сразу после StartSave = %d, want 0 — снапшот не копирует блоки", got)
	}
	w.SetBlock(1, 1, 2)
	w.SetBlock(2, 2, 2) // тот же чанк (0,0) — уже копия
	w.SetBlock(-5, -5, 2)
	w.SetBlock(100, 100, 2) // новый чанк
	w.SetBlock(101, 100, 2)
	if got := w.Copies(); got != 2 {
		t.Fatalf("Copies = %d, want 2: (0,0) и (-1,-1) по разу, новый чанк не копируется", got)
	}
	if w.StartSave() {
		t.Fatal("StartSave во время записи = true")
	}
	w.SetBlock(3, 3, 2)
	if got := w.Copies(); got != 2 {
		t.Fatalf("Copies после отклонённого StartSave = %d, want 2", got)
	}
	if got := snap.Chunks(); got != 3 {
		t.Errorf("snap.Chunks = %d, want 3", got)
	}
	close(s.release)
	w.Wait()

	w.StartSave()
	snap2 := s.next(t)
	w.SetBlock(100, 100, 3) // чанк, созданный после первого снапшота, теперь общий
	w.SetBlock(20, 1, 3)    // (1,0) не трогали со времён первого снапшота
	if got := w.Copies(); got != 4 {
		t.Fatalf("Copies после второго снапшота = %d, want 4", got)
	}
	w.Wait()
	if got := snap2.Chunks(); got != 4 {
		t.Errorf("snap2.Chunks = %d, want 4", got)
	}
	if got := snap2.Block(100, 100); got != 2 {
		t.Errorf("snap2.Block(100, 100) = %d, want 2", got)
	}
	// Старые снапшоты не меняются и после следующих сохранений.
	if got := snap.Block(1, 1); got != 0 {
		t.Errorf("snap.Block(1, 1) = %d, want 0", got)
	}
	if got := snap.Block(100, 100); got != 0 {
		t.Errorf("snap.Block(100, 100) = %d, want 0", got)
	}
	if got := snap.Block(0, 0); got != 1 {
		t.Errorf("snap.Block(0, 0) = %d, want 1", got)
	}
}

// Отрицательные координаты: чанк — floor(x/16), а не x/16.
func TestHiddenNegativeChunks(t *testing.T) {
	w := NewWorld(func(*Snapshot) error { return nil })
	pts := [][2]int{{-1, 0}, {0, -1}, {-16, -16}, {-17, -17}, {15, 15}, {16, 16}, {-1, 15}, {-32, 0}, {-33, 0}}
	for i, p := range pts {
		w.SetBlock(p[0], p[1], uint16(i+1))
	}
	var snap *Snapshot
	w2 := NewWorld(func(s *Snapshot) error { snap = s; return nil })
	for i, p := range pts {
		w2.SetBlock(p[0], p[1], uint16(i+1))
	}
	w2.StartSave()
	w2.Wait()
	if snap == nil {
		t.Fatal("save не вызвана")
	}
	for i, p := range pts {
		if got := w.Block(p[0], p[1]); got != uint16(i+1) {
			t.Errorf("Block(%d, %d) = %d, want %d", p[0], p[1], got, i+1)
		}
		if got := snap.Block(p[0], p[1]); got != uint16(i+1) {
			t.Errorf("snap.Block(%d, %d) = %d, want %d", p[0], p[1], got, i+1)
		}
	}
	// (-1,0) (0,-1) (-16,-16) (-17,-17) (15,15) (16,16) (-1,15) (-32,0) (-33,0):
	// чанки (-1,0) (0,-1) (-1,-1) (-2,-2) (0,0) (1,1) (-1,0) (-2,0) (-3,0).
	if got := snap.Chunks(); got != 8 {
		t.Errorf("snap.Chunks = %d, want 8", got)
	}
	if got := snap.Block(-2, 0); got != 0 {
		t.Errorf("snap.Block(-2, 0) = %d, want 0", got)
	}
}

func TestHiddenEmptyWorldAndTick(t *testing.T) {
	var snap *Snapshot
	w := NewWorld(func(s *Snapshot) error { snap = s; return nil })
	for range 7 {
		w.Tick()
	}
	if got := w.CurrentTick(); got != 7 {
		t.Fatalf("CurrentTick = %d, want 7", got)
	}
	w.StartSave()
	w.Wait()
	if snap == nil {
		t.Fatal("save не вызвана")
	}
	if snap.Tick != 7 || snap.Chunks() != 0 || snap.Block(3, 3) != 0 {
		t.Errorf("пустой мир: Tick=%d Chunks=%d Block=%d, want 7 0 0", snap.Tick, snap.Chunks(), snap.Block(3, 3))
	}
}

// После Wait горутина записи завершена — сохранения не текут.
func TestHiddenNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	w := NewWorld(func(*Snapshot) error { return nil })
	for range 50 {
		w.SetBlock(1, 1, 1)
		w.StartSave()
		w.Wait()
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+2 {
		if time.Now().After(deadline) {
			t.Fatalf("горутин: было %d, стало %d — сохранения не завершаются", before, runtime.NumGoroutine())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// StartSave сам замечает завершившееся сохранение: Wait не нужен.
func TestHiddenStartSaveWithoutWait(t *testing.T) {
	saved := make(chan uint64, 10)
	w := NewWorld(func(s *Snapshot) error { saved <- s.Tick; return nil })
	if !w.StartSave() {
		t.Fatal("первый StartSave = false")
	}
	<-saved // save вернётся сразу после отправки
	w.Tick()
	deadline := time.Now().Add(2 * time.Second)
	for !w.StartSave() {
		if time.Now().After(deadline) {
			t.Fatal("StartSave без Wait так и не увидел, что сохранение закончилось")
		}
		time.Sleep(time.Millisecond)
	}
	if tick := <-saved; tick != 1 {
		t.Errorf("второй снапшот: тик %d, want 1", tick)
	}
	if err := w.Wait(); err != nil {
		t.Errorf("Wait = %v", err)
	}
}

// Несколько снапшотов подряд без записей — одна копия на первую запись.
func TestHiddenRepeatedSavesOneCopy(t *testing.T) {
	w := NewWorld(func(*Snapshot) error { return nil })
	w.SetBlock(1, 1, 1)
	for range 3 {
		w.StartSave()
		w.Wait()
	}
	w.SetBlock(2, 2, 2)
	w.SetBlock(3, 3, 3)
	if got := w.Copies(); got != 1 {
		t.Fatalf("Copies = %d, want 1", got)
	}
}

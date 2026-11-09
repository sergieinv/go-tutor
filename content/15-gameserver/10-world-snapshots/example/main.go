// Мир сохраняется снапшотами, а тик при этом не останавливается.
// В тике — дешёвая копия: список указателей на чанки и номер
// поколения. Запись на диск — в отдельной горутине. Чанк, который
// попал в снапшот, больше не меняется: первая запись в него после
// снапшота копирует чанк (copy-on-write). Медленный диск изображает
// канал `slowDisk`, поэтому вывод не зависит от скорости машины.
package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
)

type chunkPos struct{ X, Z int32 }

// Чанк 16×16 блоков. `gen` — поколение, в котором чанк создан
// или скопирован: если оно старше текущего, на чанк может
// смотреть снапшот, и менять его на месте нельзя.
type chunk struct {
	pos    chunkPos
	gen    uint64
	blocks [16 * 16]uint16
}

// Мир целиком принадлежит горутине тика: ни мьютексов, ни атомиков.
type world struct {
	tick   uint64
	gen    uint64
	chunks map[chunkPos]*chunk
	copies int // сколько раз сработал copy-on-write
}

// Сдвиг `>> 4` — это floor(x/16) и для отрицательных: -1 >> 4 == -1.
// `& 15` — координата внутри чанка, тоже без отрицательных.
func (w *world) setBlock(x, z int, b uint16) {
	pos := chunkPos{int32(x >> 4), int32(z >> 4)}
	c := w.chunks[pos]
	switch {
	case c == nil:
		c = &chunk{pos: pos, gen: w.gen}
		w.chunks[pos] = c
	case c.gen != w.gen:
		cp := *c // 512 байт блоков копируются один раз за снапшот
		cp.gen = w.gen
		c = &cp
		w.chunks[pos] = c
		w.copies++
	}
	c.blocks[(z&15)*16+(x&15)] = b
}

func (w *world) block(x, z int) uint16 {
	if c := w.chunks[chunkPos{int32(x >> 4), int32(z >> 4)}]; c != nil {
		return c.blocks[(z&15)*16+(x&15)]
	}
	return 0
}

// Снапшот в тике: O(число чанков) указателей, ни одного блока.
// После `gen++` все существующие чанки — общие со снапшотом.
type snapshot struct {
	tick   uint64
	chunks []*chunk
}

func (w *world) snapshot() *snapshot {
	s := &snapshot{tick: w.tick, chunks: make([]*chunk, 0, len(w.chunks))}
	for _, c := range w.chunks {
		s.chunks = append(s.chunks, c)
	}
	// Обход map случаен — сортируем, чтобы файл был одинаковым.
	slices.SortFunc(s.chunks, func(a, b *chunk) int {
		return cmp.Or(cmp.Compare(a.pos.X, b.pos.X), cmp.Compare(a.pos.Z, b.pos.Z))
	})
	w.gen++
	return s
}

// Формат: магия, тик, число чанков, чанки (big-endian), CRC32
// в конце — оборванный или испорченный файл не загрузится.
func encode(s *snapshot) []byte {
	buf := []byte("GTW1")
	buf = binary.BigEndian.AppendUint64(buf, s.tick)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(s.chunks)))
	for _, c := range s.chunks {
		buf = binary.BigEndian.AppendUint32(buf, uint32(c.pos.X))
		buf = binary.BigEndian.AppendUint32(buf, uint32(c.pos.Z))
		for _, b := range c.blocks {
			buf = binary.BigEndian.AppendUint16(buf, b)
		}
	}
	return binary.BigEndian.AppendUint32(buf, crc32.ChecksumIEEE(buf))
}

func decode(data []byte) (tick uint64, chunks int, blocks map[[2]int]uint16, err error) {
	const chunkBytes = 8 + 16*16*2
	if len(data) < 20 || !bytes.Equal(data[:4], []byte("GTW1")) {
		return 0, 0, nil, errors.New("не файл мира")
	}
	body, sum := data[:len(data)-4], binary.BigEndian.Uint32(data[len(data)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return 0, 0, nil, errors.New("контрольная сумма не сошлась")
	}
	tick, n := binary.BigEndian.Uint64(body[4:]), binary.BigEndian.Uint32(body[12:])
	rest := body[16:]
	if uint64(len(rest)) != uint64(n)*chunkBytes {
		return 0, 0, nil, errors.New("размер не сходится с числом чанков")
	}
	blocks = map[[2]int]uint16{}
	for range n {
		cx, cz := int(int32(binary.BigEndian.Uint32(rest))), int(int32(binary.BigEndian.Uint32(rest[4:])))
		for i := range 256 {
			if b := binary.BigEndian.Uint16(rest[8+2*i:]); b != 0 {
				blocks[[2]int{cx*16 + i%16, cz*16 + i/16}] = b
			}
		}
		rest = rest[chunkBytes:]
	}
	return tick, int(n), blocks, nil
}

// Атомарная запись: временный файл в том же каталоге, `Sync`,
// `Rename` поверх старого. Упадём посередине — останется старый
// файл целиком и мусорный `*.tmp-*`, но не половина мира.
func writeFileAtomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Сохранятель: флаг `busy` читает и пишет только горутина тика,
// а горутина записи сообщает о конце через канал `done`.
// Второе сохранение, пока идёт первое, не запускается.
type saver struct {
	path     string
	busy     bool
	err      error // результат последнего завершённого сохранения
	done     chan error
	slowDisk chan struct{} // nil — диск быстрый
}

// poll — неблокирующая проверка в начале тика: если запись
// закончилась, забрать результат и снять флаг. Ждать не нужно.
func (s *saver) poll() {
	if !s.busy {
		return
	}
	select {
	case s.err = <-s.done:
		s.busy = false
	default:
	}
}

// start снимает снапшот (take) только если можно писать: при
// отказе мир не трогается — ни снапшота, ни нового поколения.
func (s *saver) start(take func() *snapshot) bool {
	s.poll()
	if s.busy {
		return false
	}
	s.busy = true
	snap := take()
	go func() {
		if s.slowDisk != nil {
			<-s.slowDisk
		}
		s.done <- writeFileAtomic(s.path, encode(snap))
	}()
	return true
}

// wait блокируется до конца записи. Тик его не зовёт — хватает
// poll; здесь ждём явно, чтобы вывод был детерминированным,
// и при остановке — перед финальным снапшотом.
func (s *saver) wait() error {
	if s.busy {
		s.err = <-s.done
		s.busy = false
	}
	return s.err
}

func main() {
	dir, _ := os.MkdirTemp("", "world")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "world.dat")
	w := &world{chunks: map[chunkPos]*chunk{}}
	sv := &saver{path: path, done: make(chan error, 1), slowDisk: make(chan struct{})}

	// Тик: два игрока ставят блоки, раз в 4 тика — автосохранение.
	step := func() {
		w.tick++
		t := int(w.tick)
		w.setBlock(t, -t, uint16(t))
		w.setBlock(-20+t, 5, uint16(100+t))
		if w.tick%4 == 0 {
			if sv.start(w.snapshot) {
				fmt.Printf("тик %d: снапшот отдан на запись (чанков %d)\n", w.tick, len(w.chunks))
			} else {
				fmt.Printf("тик %d: прошлое сохранение ещё пишется — пропуск\n", w.tick)
			}
		}
	}
	for range 8 {
		step()
	}
	copiesBefore := w.copies
	close(sv.slowDisk) // диск «прожевал» запись
	fmt.Println("запись завершена, ошибка:", sv.wait())
	fmt.Printf("копий чанков за тики 5–8: %d из %d — копируются только тронутые\n", copiesBefore, len(w.chunks))

	tick, n, blocks, err := decode(must(os.ReadFile(path)))
	fmt.Printf("в файле: тик %d, чанков %d, ошибка %v\n", tick, n, err)
	fmt.Printf("блок (3,-3): в файле %d, в мире %d\n", blocks[[2]int{3, -3}], w.block(3, -3))
	fmt.Printf("блок (6,-6): в файле %d, в мире %d — поставлен после снапшота\n", blocks[[2]int{6, -6}], w.block(6, -6))

	// Graceful shutdown: SIGTERM отменяет контекст, цикл дотикивает
	// текущий тик, перестаёт принимать игроков, отключает их
	// и пишет финальный снапшот — синхронно, дождавшись прошлого.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	sv.slowDisk = nil
	ticks := make(chan struct{})
	go func() {
		for range 3 {
			ticks <- struct{}{}
		}
		syscall.Kill(os.Getpid(), syscall.SIGTERM) // «оркестратор» останавливает под
	}()
loop:
	for {
		select {
		case <-ticks:
			step()
		case <-ctx.Done():
			break loop
		}
	}
	fmt.Println("SIGTERM после тика", w.tick)
	fmt.Println("  приём новых соединений остановлен, игроки отключены")
	if err := sv.wait(); err != nil {
		fmt.Println("  прошлое сохранение упало:", err)
	}
	sv.start(w.snapshot)
	fmt.Println("  финальный снапшот записан, ошибка:", sv.wait())

	tick, _, blocks, _ = decode(must(os.ReadFile(path)))
	fmt.Printf("после перезапуска: тик %d, блок (11,-11) = %d\n", tick, blocks[[2]int{11, -11}])
	left, _ := filepath.Glob(filepath.Join(dir, "*.tmp-*"))
	fmt.Println("временных файлов осталось:", len(left))
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

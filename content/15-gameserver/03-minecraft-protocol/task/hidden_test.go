package task

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"math/rand/v2"
	"runtime"
	"testing"
	"testing/iotest"
)

func zlibOf(b []byte) []byte {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(b)
	zw.Close()
	return z.Bytes()
}

// frame собирает кадр вручную: длина данных и «хвост» как есть.
func frame(dataLen int32, rest []byte) []byte {
	body := append(tAppendVarInt(nil, dataLen), rest...)
	return append(tAppendVarInt(nil, int32(len(body))), body...)
}

func TestHiddenThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		size, threshold int
		compressed      bool
	}{
		{255, 256, false},
		{256, 256, true},
		{257, 256, true},
		{1, 0, true},
		{1, 1, true},
		{1, 2, false},
	} {
		packet := bytes.Repeat([]byte{0x42}, tc.size)
		f, err := AppendCompressed(nil, packet, tc.threshold)
		if err != nil {
			t.Fatalf("размер %d, порог %d: %v", tc.size, tc.threshold, err)
		}
		r := bytes.NewReader(f)
		tReadVarInt(r)
		dl, _ := tReadVarInt(r)
		if (dl != 0) != tc.compressed {
			t.Errorf("размер %d, порог %d: длина данных %d, сжатие ожидалось: %v", tc.size, tc.threshold, dl, tc.compressed)
		}
		if tc.compressed && int(dl) != tc.size {
			t.Errorf("размер %d: длина данных %d", tc.size, dl)
		}
		back, err := ReadCompressed(bytes.NewReader(f), tc.threshold, 1<<20)
		if err != nil || !bytes.Equal(back, packet) {
			t.Errorf("размер %d, порог %d: round-trip %v", tc.size, tc.threshold, err)
		}
	}
}

func TestHiddenAppendKeepsDst(t *testing.T) {
	dst := []byte("prefix")
	out, err := AppendCompressed(dst, bytes.Repeat([]byte{1}, 500), 256)
	if err != nil || !bytes.HasPrefix(out, []byte("prefix")) {
		t.Fatalf("префикс dst потерян: %v", err)
	}
	// Слишком большой несжатый кадр: ErrTooLarge, dst без изменений.
	huge := make([]byte, MaxPacketLength)
	out, err = AppendCompressed([]byte("x"), huge, MaxPacketLength+10)
	if !errors.Is(err, ErrTooLarge) || string(out) != "x" {
		t.Fatalf("кадр больше %d: %d байт, %v; want dst без изменений и ErrTooLarge", MaxPacketLength, len(out), err)
	}
	// Ровно в предел: тело = 1 байт длины данных + пакет.
	edge := make([]byte, MaxPacketLength-1)
	out, err = AppendCompressed(nil, edge, MaxPacketLength+10)
	if err != nil {
		t.Fatalf("кадр ровно %d байт: %v", MaxPacketLength, err)
	}
	back, err := ReadCompressed(bytes.NewReader(out), MaxPacketLength+10, 1<<20)
	if err != nil || len(back) != len(edge) {
		t.Fatalf("чтение кадра в предел: %d, %v", len(back), err)
	}
}

func TestHiddenFrameErrors(t *testing.T) {
	z := zlibOf(bytes.Repeat([]byte{'a'}, 300))
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"обрыв длины", []byte{0x80}, io.ErrUnexpectedEOF},
		{"длина 6 байт", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, ErrVarInt},
		{"длина 0", []byte{0x00}, ErrBadLength},
		{"длина -1", []byte{0xff, 0xff, 0xff, 0xff, 0x0f}, ErrBadLength},
		{"длина больше 2097151, тела нет", tAppendVarInt(nil, MaxPacketLength+1), ErrTooLarge},
		{"обрыв тела", []byte{0x05, 0x00, 0x01}, io.ErrUnexpectedEOF},
		{"нет пакета после длины данных 0", []byte{0x01, 0x00}, ErrBadLength},
		{"обрыв длины данных", []byte{0x01, 0x80}, ErrBadLength},
		{"длина данных 6 байт", []byte{0x06, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, ErrVarInt},
		{"отрицательная длина данных", frame(-5, z), ErrBadLength},
		{"сжатый меньше порога", frame(255, zlibOf(make([]byte, 255))), ErrBelowThreshold},
		{"битый заголовок zlib", frame(300, []byte{0x12, 0x34, 0x56, 0x78}), ErrBadCompression},
		{"обрыв zlib-потока", frame(300, z[:len(z)/2]), ErrBadCompression},
		{"пустой zlib-поток", frame(300, nil), ErrBadCompression},
		{"распаковано меньше объявленного", frame(400, z), ErrSizeMismatch},
		{"распаковано больше объявленного", frame(299, z), ErrSizeMismatch},
	}
	for _, tt := range tests {
		got, err := ReadCompressed(bytes.NewReader(tt.in), 256, 1<<20)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
		if err != nil && got != nil {
			t.Errorf("%s: при ошибке результат должен быть nil", tt.name)
		}
	}
	// Испорченная контрольная сумма Adler-32 в конце потока.
	bad := append([]byte(nil), z...)
	bad[len(bad)-1] ^= 0xff
	if _, err := ReadCompressed(bytes.NewReader(frame(300, bad)), 256, 1<<20); !errors.Is(err, ErrBadCompression) {
		t.Errorf("плохая контрольная сумма: err = %v, want ErrBadCompression", err)
	}
}

// Объявленная длина данных больше maxData — отказ до распаковки:
// хвост — мусор, а ошибка всё равно ErrTooLarge, а не ErrBadCompression.
func TestHiddenMaxDataBeforeInflate(t *testing.T) {
	f := frame(1<<21, []byte{0xde, 0xad, 0xbe, 0xef})
	if _, err := ReadCompressed(bytes.NewReader(f), 256, 1<<20); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// Ровно maxData — допустимо.
	packet := bytes.Repeat([]byte{9}, 4096)
	f2, _ := AppendCompressed(nil, packet, 256)
	if got, err := ReadCompressed(bytes.NewReader(f2), 256, 4096); err != nil || !bytes.Equal(got, packet) {
		t.Fatalf("ровно maxData: %v", err)
	}
	// Несжатые пакеты maxData не ограничивает и порог не проверяется.
	big := bytes.Repeat([]byte{1}, 5000)
	f3 := frame(0, big)
	if got, err := ReadCompressed(bytes.NewReader(f3), 256, 100); err != nil || !bytes.Equal(got, big) {
		t.Fatalf("несжатый пакет больше порога и maxData: %v", err)
	}
}

// zlib-бомба: 16 МиБ нулей сжаты в килобайты, объявлено 1000 байт.
// Распаковывать дальше объявленного нельзя.
func TestHiddenZipBomb(t *testing.T) {
	bomb := zlibOf(make([]byte, 16<<20))
	f := frame(1000, bomb)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range 3 {
		if _, err := ReadCompressed(bytes.NewReader(f), 256, 1<<20); !errors.Is(err, ErrSizeMismatch) {
			t.Fatalf("err = %v, want ErrSizeMismatch", err)
		}
	}
	runtime.ReadMemStats(&after)
	if d := after.TotalAlloc - before.TotalAlloc; d > 4<<20 {
		t.Fatalf("на три бомбы выделено %d байт — распаковка не остановилась на объявленном размере", d)
	}
}

// Кадры подряд из одного потока: ReadCompressed не читает лишнего.
func TestHiddenStream(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	var wire []byte
	var packets [][]byte
	for range 40 {
		p := make([]byte, 1+rng.IntN(1500))
		for i := range p {
			p[i] = byte('a' + rng.IntN(3)) // сжимается
		}
		packets = append(packets, p)
		var err error
		if wire, err = AppendCompressed(wire, p, 256); err != nil {
			t.Fatal(err)
		}
	}
	for name, r := range map[string]Reader{
		"bytes":    bytes.NewReader(wire),
		"по байту": bufio.NewReaderSize(iotest.OneByteReader(bytes.NewReader(wire)), 16),
		"DataErr":  bufio.NewReader(iotest.DataErrReader(bytes.NewReader(wire))),
	} {
		for i, want := range packets {
			got, err := ReadCompressed(r, 256, 1<<20)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s: пакет %d: %v", name, i, err)
			}
		}
		if _, err := ReadCompressed(r, 256, 1<<20); err != io.EOF {
			t.Fatalf("%s: после последнего кадра err = %v, want io.EOF", name, err)
		}
	}
}

// Случайные байты: только ошибки из списка, никаких паник.
func TestHiddenRandomInput(t *testing.T) {
	known := []error{io.EOF, io.ErrUnexpectedEOF, ErrVarInt, ErrBadLength, ErrTooLarge, ErrBelowThreshold, ErrBadCompression, ErrSizeMismatch}
	rng := rand.New(rand.NewPCG(5, 6))
	good, err := AppendCompressed(nil, bytes.Repeat([]byte("xyz"), 200), 64)
	if err != nil || len(good) == 0 {
		t.Fatalf("AppendCompressed: %v", err)
	}
	for i := range 5000 {
		data := append([]byte(nil), good...)
		for range 1 + rng.IntN(4) {
			data[rng.IntN(len(data))] = byte(rng.Uint32())
		}
		data = data[:rng.IntN(len(data)+1)]
		_, err := ReadCompressed(bytes.NewReader(data), 64, 1<<16)
		if err == nil {
			continue
		}
		ok := false
		for _, k := range known {
			ok = ok || errors.Is(err, k)
		}
		if !ok {
			t.Fatalf("вход %d: неожиданная ошибка %v", i, err)
		}
	}
}

// Байты тела после конца zlib-потока игнорируются.
func TestHiddenTrailingAfterZlib(t *testing.T) {
	packet := bytes.Repeat([]byte{'q'}, 300)
	f := frame(300, append(zlibOf(packet), 0xde, 0xad))
	got, err := ReadCompressed(bytes.NewReader(f), 256, 1<<20)
	if err != nil || !bytes.Equal(got, packet) {
		t.Fatalf("хвост после zlib-потока: %v; want пакет без ошибки", err)
	}
}

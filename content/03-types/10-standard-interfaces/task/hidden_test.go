package task

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// shortWriter принимает не больше limit байт за вызов и возвращает ошибку.
type shortWriter struct {
	limit int
	got   []byte
}

var errShort = errors.New("short write")

func (s *shortWriter) Write(p []byte) (int, error) {
	if len(p) <= s.limit {
		s.got = append(s.got, p...)
		return len(p), nil
	}
	s.got = append(s.got, p[:s.limit]...)
	return s.limit, errShort
}

func TestCountingWriterShortWrite(t *testing.T) {
	sw := &shortWriter{limit: 3}
	cw := &CountingWriter{W: sw}
	n, err := cw.Write([]byte("a\nbc\nd\n"))
	if n != 3 || !errors.Is(err, errShort) {
		t.Errorf("Write = %d, %v; want 3, errShort", n, err)
	}
	if cw.Bytes != 3 || cw.Lines != 1 {
		t.Errorf("Bytes, Lines = %d, %d; want 3, 1 — считать только записанное", cw.Bytes, cw.Lines)
	}
}

func TestCountingWriterEmpty(t *testing.T) {
	cw := &CountingWriter{W: io.Discard}
	if n, err := cw.Write(nil); n != 0 || err != nil {
		t.Errorf("Write(nil) = %d, %v; want 0, nil", n, err)
	}
	if cw.Bytes != 0 || cw.Lines != 0 {
		t.Errorf("Bytes, Lines = %d, %d; want 0, 0", cw.Bytes, cw.Lines)
	}
}

func TestUpperReaderOneByte(t *testing.T) {
	got, err := io.ReadAll(UpperReader{iotest.OneByteReader(strings.NewReader("abc-xyz"))})
	if err != nil || string(got) != "ABC-XYZ" {
		t.Errorf("ReadAll(OneByteReader) = %q, %v; want \"ABC-XYZ\", nil", got, err)
	}
}

func TestUpperReaderDataWithEOF(t *testing.T) {
	// DataErrReader возвращает последние данные вместе с io.EOF.
	r := UpperReader{iotest.DataErrReader(strings.NewReader("go"))}
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "GO" {
		t.Errorf("ReadAll(DataErrReader) = %q, %v; want \"GO\", nil", got, err)
	}
}

func TestUpperReaderNonASCII(t *testing.T) {
	got, _ := io.ReadAll(UpperReader{strings.NewReader("привет, z{|}")})
	if string(got) != "привет, Z{|}" {
		t.Errorf("прочитано %q, want %q", got, "привет, Z{|}")
	}
}

func TestUpperReaderPassesErrors(t *testing.T) {
	boom := errors.New("boom")
	n, err := UpperReader{iotest.ErrReader(boom)}.Read(make([]byte, 4))
	if n != 0 || !errors.Is(err, boom) {
		t.Errorf("Read = %d, %v; want 0, boom", n, err)
	}
}

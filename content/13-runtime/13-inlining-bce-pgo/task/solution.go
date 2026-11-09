package task

import (
	"encoding/binary"
	"errors"
)

// Ошибки разбора кадра.
var (
	ErrShort   = errors.New("frame: short buffer")
	ErrMagic   = errors.New("frame: bad magic")
	ErrVersion = errors.New("frame: unsupported version")
)

// Frame — разобранный кадр. Payload указывает в исходный буфер.
type Frame struct {
	Flags   byte
	Payload []byte
}

const headerLen = 6

// Parse разбирает один кадр в начале b и возвращает его и число
// прочитанных байт.
func Parse(b []byte) (Frame, int, error) {
	// Явная проверка длины вместо паники: после неё компилятор
	// знает, что len(b) >= 6, и b[0]..b[5] обходятся без проверок.
	if len(b) < headerLen {
		return Frame{}, 0, ErrShort
	}
	if b[0] != 'G' || b[1] != 'T' {
		return Frame{}, 0, ErrMagic
	}
	if b[2] != 1 {
		return Frame{}, 0, ErrVersion
	}
	n := headerLen + int(binary.BigEndian.Uint16(b[4:6]))
	if len(b) < n {
		return Frame{}, 0, ErrShort
	}
	// Полное выражение среза: cap == len, и append к Payload
	// не перезапишет следующий кадр в том же буфере.
	return Frame{Flags: b[3], Payload: b[headerLen:n:n]}, n, nil
}

// Walk разбирает кадры подряд, вызывая fn для каждого, и возвращает
// число байт в разобранных кадрах.
func Walk(b []byte, fn func(Frame)) (int, error) {
	done := 0
	for done < len(b) {
		f, n, err := Parse(b[done:])
		if err != nil {
			return done, err
		}
		if fn != nil {
			fn(f)
		}
		done += n
	}
	return done, nil
}

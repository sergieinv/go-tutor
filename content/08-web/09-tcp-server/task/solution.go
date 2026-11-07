package task

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ErrFrameTooLarge — длина кадра больше допустимой.
var ErrFrameTooLarge = errors.New("frame too large")

// WriteFrame пишет кадр: 4 байта длины (big-endian), затем payload.
func WriteFrame(w io.Writer, payload []byte, max int) error {
	if len(payload) > max {
		return fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(payload), max)
	}
	// Заголовок и данные — одним Write: меньше системных вызовов,
	// и кадры из разных горутин не перемешаются на уровне Write.
	buf := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(len(payload)))
	copy(buf[4:], payload)
	_, err := w.Write(buf)
	return err
}

// ReadFrame читает один кадр, записанный WriteFrame.
func ReadFrame(r io.Reader, max int) ([]byte, error) {
	var hdr [4]byte
	// ReadFull: один Read может вернуть меньше байт, чем просили.
	// Ничего не прочитано — io.EOF, часть — io.ErrUnexpectedEOF.
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if uint64(n) > uint64(max) {
		// Память под кадр не выделяем: длину прислал клиент.
		return nil, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, max)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF // заголовок был, данных нет
		}
		return nil, err
	}
	return payload, nil
}

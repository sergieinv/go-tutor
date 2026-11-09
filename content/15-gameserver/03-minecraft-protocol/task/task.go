package task

import (
	"errors"
	"io"
)

const (
	MaxPacketLength = 2097151 // 2²¹−1: длина кадра — не больше 3 байт VarInt
	MaxVarIntLen    = 5
)

var (
	ErrVarInt         = errors.New("bad varint")
	ErrBadLength      = errors.New("bad length")
	ErrTooLarge       = errors.New("packet too large")
	ErrBelowThreshold = errors.New("compressed packet below threshold")
	ErrBadCompression = errors.New("bad zlib data")
	ErrSizeMismatch   = errors.New("decompressed size mismatch")
)

var errNotImplemented = errors.New("not implemented")

// Reader — то, из чего читаются кадры: bufio.Reader, bytes.Reader.
type Reader interface {
	io.Reader
	io.ByteReader
}

// AppendCompressed дописывает к dst кадр с пакетом packet (id + данные)
// в формате протокола со сжатием.
func AppendCompressed(dst, packet []byte, threshold int) ([]byte, error) {
	// TODO
	return dst, errNotImplemented
}

// ReadCompressed читает один кадр и возвращает пакет (id + данные).
func ReadCompressed(r Reader, threshold, maxData int) ([]byte, error) {
	// TODO
	return nil, errNotImplemented
}

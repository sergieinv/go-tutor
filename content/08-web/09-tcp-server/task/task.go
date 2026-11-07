package task

import (
	"errors"
	"io"
)

// ErrFrameTooLarge — длина кадра больше допустимой.
var ErrFrameTooLarge = errors.New("frame too large")

// WriteFrame пишет кадр: 4 байта длины (big-endian), затем payload.
func WriteFrame(w io.Writer, payload []byte, max int) error {
	// TODO: реализуйте функцию
	return nil
}

// ReadFrame читает один кадр, записанный WriteFrame.
func ReadFrame(r io.Reader, max int) ([]byte, error) {
	// TODO: реализуйте функцию
	return nil, nil
}

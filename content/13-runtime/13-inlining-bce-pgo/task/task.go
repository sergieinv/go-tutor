package task

import "errors"

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

// Parse разбирает один кадр в начале b и возвращает его и число
// прочитанных байт.
func Parse(b []byte) (Frame, int, error) {
	// TODO: реализуйте функцию
	return Frame{}, 0, nil
}

// Walk разбирает кадры подряд, вызывая fn для каждого, и возвращает
// число байт в разобранных кадрах.
func Walk(b []byte, fn func(Frame)) (int, error) {
	// TODO: реализуйте функцию
	return 0, nil
}

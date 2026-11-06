package task

import "io"

// CountingWriter пересылает запись в W и считает байты и строки.
type CountingWriter struct {
	W     io.Writer
	Bytes int64
	Lines int
}

func (c *CountingWriter) Write(p []byte) (int, error) {
	// TODO: реализуйте метод
	return 0, nil
}

// UpperReader читает из R, переводя латинские a–z в верхний регистр.
type UpperReader struct {
	R io.Reader
}

func (u UpperReader) Read(p []byte) (int, error) {
	// TODO: реализуйте метод
	return 0, io.EOF
}

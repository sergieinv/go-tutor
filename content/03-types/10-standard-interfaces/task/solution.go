package task

import (
	"bytes"
	"io"
)

// CountingWriter пересылает запись в W и считает байты и строки.
type CountingWriter struct {
	W     io.Writer
	Bytes int64
	Lines int
}

func (c *CountingWriter) Write(p []byte) (int, error) {
	n, err := c.W.Write(p)
	// Считаем только то, что реально записано.
	c.Bytes += int64(n)
	c.Lines += bytes.Count(p[:n], []byte{'\n'})
	return n, err
}

// UpperReader читает из R, переводя латинские a–z в верхний регистр.
type UpperReader struct {
	R io.Reader
}

func (u UpperReader) Read(p []byte) (int, error) {
	n, err := u.R.Read(p)
	// Данные могут прийти вместе с ошибкой — обрабатываем p[:n] всегда.
	for i, b := range p[:n] {
		if 'a' <= b && b <= 'z' {
			p[i] = b - 'a' + 'A'
		}
	}
	return n, err
}

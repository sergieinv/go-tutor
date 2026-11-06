package task

import "io"

// WriteAtomic записывает файл path целиком или не трогает его вовсе.
func WriteAtomic(path string, write func(w io.Writer) error) error {
	// TODO: реализуйте функцию
	return nil
}

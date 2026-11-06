// Чтение файлов в Go строится на одном интерфейсе —
// `io.Reader`. Файл, строка в памяти, тело HTTP-ответа
// и сетевое соединение читаются одним и тем же кодом.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Функция принимает любой `io.Reader`, а не `*os.File`:
// её можно вызвать и для файла, и для строки в тестах.
func countWords(r io.Reader) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Split(bufio.ScanWords) // режем по словам, а не по строкам
	n := 0
	for sc.Scan() {
		n++
	}
	return n, sc.Err()
}

func main() {
	// Сначала создадим файл, чтобы было что читать.
	const name = "notes.txt"
	text := "первая строка\nвторая строка\r\nтретья\n"
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		panic(err)
	}
	defer os.Remove(name)

	// `os.ReadFile` читает файл целиком в `[]byte`.
	// Удобно для небольших файлов: конфигов, шаблонов.
	data, err := os.ReadFile(name)
	if err != nil {
		panic(err)
	}
	fmt.Printf("ReadFile: %d байт\n", len(data))

	// Для больших файлов — `os.Open` и чтение потоком.
	// `Close` откладываем сразу после проверки ошибки.
	f, err := os.Open(name)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// `bufio.Scanner` отдаёт строки без `\n` (и без `\r`
	// перед ним). Ошибку чтения проверяют после цикла.
	sc := bufio.NewScanner(f)
	for i := 1; sc.Scan(); i++ {
		fmt.Printf("%d: %q\n", i, sc.Text())
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}

	// Тот же `countWords` работает и с файлом, и со строкой.
	f.Seek(0, io.SeekStart) // вернуться в начало файла
	n, _ := countWords(f)
	m, _ := countWords(strings.NewReader("раз два три"))
	fmt.Println("слов в файле:", n, "в строке:", m)

	// Строка длиннее 64 КБ не влезет в буфер Scanner'а:
	// `Scan` вернёт false, а `Err` — `bufio.ErrTooLong`.
	long := strings.Repeat("x", 100_000)
	sc = bufio.NewScanner(strings.NewReader(long))
	for sc.Scan() {
	}
	fmt.Println("ErrTooLong:", errors.Is(sc.Err(), bufio.ErrTooLong))

	// Лимит поднимают методом `Buffer` до первого `Scan`.
	sc = bufio.NewScanner(strings.NewReader(long))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	sc.Scan()
	fmt.Println("с Buffer:", len(sc.Text()), sc.Err())

	// `bufio.Reader` читает до разделителя и оставляет его
	// в результате. В конце потока приходит `io.EOF`.
	br := bufio.NewReader(strings.NewReader("a,b,c"))
	for {
		part, err := br.ReadString(',')
		fmt.Printf("%q ", part)
		if err == io.EOF {
			break
		}
	}
	fmt.Println()

	// `io.ReadAll` дочитывает любой Reader до конца,
	// `io.LimitReader` ограничивает, сколько можно прочитать.
	head, _ := io.ReadAll(io.LimitReader(strings.NewReader("abcdef"), 3))
	fmt.Println("первые 3 байта:", string(head))
}

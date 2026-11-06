// Запись — зеркало чтения: `os.WriteFile` для «записать
// всё сразу», `os.Create`/`os.OpenFile` и `io.Writer`
// для потока, `bufio.Writer` для множества мелких записей.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func size(name string) int64 {
	st, err := os.Stat(name)
	if err != nil {
		return -1
	}
	return st.Size()
}

// Атомарная запись: пишем во временный файл в том же
// каталоге и переименовываем. Читатель увидит либо
// старый файл, либо новый целиком — но не половину.
func writeAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(tmp.Name()) // при сбое не оставляем мусор
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// Ошибку `Close` при записи проверяем обязательно:
	// данные могли не дойти до диска.
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func main() {
	// `os.WriteFile` создаёт файл или обрезает существующий.
	// Последний аргумент — права для нового файла.
	os.WriteFile("log.txt", []byte("start\n"), 0o644)

	// `OpenFile` с флагами — дописать в конец, создав
	// файл при необходимости. Флаги объединяются через `|`.
	f, _ := os.OpenFile("log.txt", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	fmt.Fprintln(f, "append 1") // *os.File — это io.Writer
	f.Close()
	fmt.Println("после append:", size("log.txt"))

	// `O_EXCL` вместе с `O_CREATE`: ошибка, если файл уже есть.
	_, err := os.OpenFile("log.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	fmt.Println("O_EXCL:", errors.Is(err, fs.ErrExist))

	// `bufio.Writer` копит данные в буфере (4 КБ) и пишет
	// крупными кусками. Пока не вызван `Flush`, на диске пусто.
	out, _ := os.Create("data.txt") // = O_RDWR|O_CREATE|O_TRUNC, 0666
	w := bufio.NewWriter(out)
	for i := range 3 {
		fmt.Fprintf(w, "строка %d\n", i)
	}
	fmt.Println("до Flush:", size("data.txt"))
	if err := w.Flush(); err != nil {
		panic(err)
	}
	fmt.Println("после Flush:", size("data.txt"))
	out.Close()

	if err := writeAtomic("config.json", []byte(`{"v":1}`)); err != nil {
		panic(err)
	}
	b, _ := os.ReadFile("config.json")
	fmt.Println("config.json:", string(b))

	for _, name := range []string{"log.txt", "data.txt", "config.json"} {
		os.Remove(name)
	}
}

// Внешний ввод в команде и в пути к файлу — две границы
// доверия, на которых Go ничем не помогает сам: строка
// остаётся строкой, пока вы не передали её оболочке,
// программе-флагопарсеру или файловой системе.

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Пример перезапускает сам себя в роли внешней утилиты
// `archive [-delete] файл...`: флаг `-delete` удаляет
// исходники. Утилита разбирает аргументы пакетом `flag`, как
// большинство Go-программ.
func archiveTool(args []string) {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	del := fs.Bool("delete", false, "удалить исходники")
	fs.Parse(args)
	fmt.Printf("  archive: delete=%v файлы=%q\n", *del, fs.Args())
}

func runArchive(args ...string) {
	exe, _ := os.Executable()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "ARCHIVE_TOOL=1")
	out, _ := cmd.Output()
	fmt.Print(string(out))
}

// УЯЗВИМО: имя вставлено в скрипт оболочки — `;`, `|`, `$(…)`
// в нём выполнят произвольную команду с правами сервиса.
func greetUnsafe(name string) string {
	out, _ := exec.Command("/bin/sh", "-c", "echo привет, "+name).Output()
	return string(out)
}

// Без оболочки каждое значение — отдельный элемент argv:
// `;` и `$(…)` доходят до `echo` как обычные символы.
func greet(name string) string {
	out, _ := exec.Command("echo", "привет,", name).Output()
	return string(out)
}

// УЯЗВИМО: `Join` склеивает и чистит путь, `..` честно уводит
// наверх, а симлинк внутри каталога ведёт куда угодно.
func readUnsafe(dir, name string) error {
	_, err := os.ReadFile(filepath.Join(dir, name))
	return err
}

// `os.Root` открывает каждый элемент пути относительно
// дескриптора каталога и не выпускает за его пределы — ни
// через `..`, ни через абсолютный путь, ни через симлинк.
func readSafe(root *os.Root, name string) error {
	_, err := root.ReadFile(name)
	return err
}

func main() {
	if os.Getenv("ARCHIVE_TOOL") == "1" {
		archiveTool(os.Args[1:])
		return
	}

	// Инъекция в оболочку: вторая команда выполнилась.
	evil := "Анна; echo ВЗЛОМ"
	fmt.Print("sh -c: ", greetUnsafe(evil))
	fmt.Print("argv:  ", greet(evil))

	// Инъекция в аргументы: оболочки нет, но значение с `-`
	// утилита приняла за флаг. `--` заканчивает флаги — всё
	// дальше считается именами файлов.
	name := "-delete"
	fmt.Println("без --:")
	runArchive(name)
	fmt.Println("с --:")
	runArchive("--", name)

	// Каталог с публичными файлами, секрет рядом и симлинк
	// изнутри наружу — например, его распаковали из архива.
	tmp, _ := os.MkdirTemp("", "files")
	defer os.RemoveAll(tmp)
	pub := filepath.Join(tmp, "public")
	os.Mkdir(pub, 0o755)
	os.WriteFile(filepath.Join(pub, "hello.txt"), []byte("привет"), 0o644)
	os.WriteFile(filepath.Join(tmp, "secret.txt"), []byte("пароль"), 0o600)
	os.Symlink("../secret.txt", filepath.Join(pub, "avatar.png"))

	root, err := os.OpenRoot(pub)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer root.Close()
	for _, n := range []string{"hello.txt", "../secret.txt", "avatar.png"} {
		fmt.Printf("%-14s IsLocal=%-5v Join: %-6s Root: %s\n",
			n, filepath.IsLocal(n), result(readUnsafe(pub, n), "прочитан"), result(readSafe(root, n), "прочитан"))
	}

	// Zip slip: имя записи в архиве — тоже внешний ввод.
	// `zip.NewReader` такие имена по умолчанию пропускает, а
	// запись через `os.Root` — нет.
	zr := makeZip("docs/a.txt", "../evil.txt")
	for _, f := range zr.File {
		err := root.MkdirAll(filepath.Dir(f.Name), 0o755)
		if err == nil {
			err = root.WriteFile(f.Name, []byte("данные"), 0o644)
		}
		fmt.Printf("распаковка %-12s %v\n", f.Name, result(err, "записан"))
	}
	_, err = os.Stat(filepath.Join(tmp, "evil.txt"))
	fmt.Println("evil.txt снаружи создан:", !errors.Is(err, os.ErrNotExist))
}

func result(err error, ok string) string {
	if err != nil {
		return err.Error()
	}
	return ok
}

func makeZip(names ...string) *zip.Reader {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, _ := zw.Create(n)
		w.Write([]byte("данные"))
	}
	zw.Close()
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	return zr
}

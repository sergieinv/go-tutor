// Временные файлы и каталоги создают `os.CreateTemp`
// и `os.MkdirTemp`. Они гарантируют уникальное имя,
// но не удаляют за собой — это ваша забота.

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Пути временных файлов случайны и зависят от машины,
// поэтому печатаем не сами пути, а то, что о них известно.
func describe(path string) string {
	base := filepath.Base(path)
	return fmt.Sprintf("prefix=%v suffix=%v in TempDir=%v",
		strings.HasPrefix(base, "upload-"),
		strings.HasSuffix(base, ".csv"),
		filepath.Dir(path) == filepath.Clean(os.TempDir()))
}

func spool() string {
	// Пустой первый аргумент — каталог `os.TempDir()`
	// (в Unix — `$TMPDIR` или `/tmp`). Звёздочка в шаблоне
	// заменяется случайной строкой, остальное сохраняется.
	f, err := os.CreateTemp("", "upload-*.csv")
	if err != nil {
		panic(err)
	}
	// `defer` выполняются в обратном порядке: сначала
	// `Close`, потом `Remove`. Файл удалится при любом выходе.
	defer os.Remove(f.Name())
	defer f.Close()

	fmt.Fprintln(f, "id,name")
	fmt.Println("CreateTemp:", describe(f.Name()))
	return f.Name()
}

func main() {
	name := spool()
	_, err := os.Stat(name)
	fmt.Println("после выхода удалён:", errors.Is(err, fs.ErrNotExist))

	// Каждый вызов даёт новое имя: файл создаётся с `O_EXCL`,
	// поэтому два процесса не получат один и тот же.
	a, _ := os.CreateTemp("", "job-*")
	b, _ := os.CreateTemp("", "job-*")
	fmt.Println("имена различаются:", a.Name() != b.Name())
	a.Close()
	b.Close()
	os.Remove(a.Name())
	os.Remove(b.Name())

	// Временный каталог — для набора файлов. Убирается
	// одним `RemoveAll` в `defer`.
	dir, err := os.MkdirTemp("", "build-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "out.bin"), []byte{1, 2, 3}, 0o644)
	entries, _ := os.ReadDir(dir)
	fmt.Println("в каталоге:", len(entries), "файл")

	// Права: файл — 0600, каталог — 0700. Временные данные
	// не видны другим пользователям машины.
	st, _ := os.Stat(dir)
	fmt.Printf("права каталога: %v\n", st.Mode().Perm())

	// Разделитель пути в шаблоне запрещён.
	_, err = os.CreateTemp("", "logs/x-*")
	fmt.Println("шаблон со слешем:", err != nil)
}

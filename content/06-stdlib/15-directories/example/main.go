// Каталоги создают, читают и обходят функциями `os` и
// `path/filepath`, а `io/fs` даёт абстракцию файловой
// системы, которую одинаково обходят на диске, в `embed`
// и в тестовой `fstest.MapFS`.

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	// `MkdirAll` создаёт всю цепочку каталогов (как `mkdir -p`)
	// и не ругается, если они уже есть.
	files := []string{
		"project/go.mod",
		"project/main.go",
		"project/internal/db/db.go",
		"project/.git/HEAD",
		"project/.git/objects/ab",
	}
	for _, f := range files {
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte("x"), 0o644)
	}

	// `ReadDir` возвращает записи одного уровня,
	// отсортированные по имени. `DirEntry` знает имя и тип
	// без лишнего системного вызова `stat`.
	entries, _ := os.ReadDir("project")
	for _, e := range entries {
		fmt.Printf("%-9s dir=%v\n", e.Name(), e.IsDir())
	}

	// `WalkDir` обходит дерево в лексическом порядке.
	// Вернув `fs.SkipDir` для каталога, пропускаем его целиком.
	fmt.Println("WalkDir:")
	filepath.WalkDir("project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // каталог не прочитался — прекращаем обход
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		rel, _ := filepath.Rel("project", path)
		fmt.Println(" ", rel)
		return nil
	})

	// `os.DirFS` превращает каталог в `fs.FS`. Пути внутри —
	// всегда со слешами и без ведущего `/`; корень — `.`.
	fsys := os.DirFS("project")
	data, _ := fs.ReadFile(fsys, "internal/db/db.go")
	fmt.Println("fs.ReadFile:", string(data))
	gofiles, _ := fs.Glob(fsys, "*.go")
	fmt.Println("fs.Glob:", gofiles)

	// Функция над `fs.FS` не знает, откуда файлы: с диска,
	// из `embed.FS`, из zip или из памяти.
	n := 0
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	fmt.Println("файлов в дереве:", n)

	// `os.Remove` удаляет только пустой каталог,
	// `os.RemoveAll` — дерево целиком.
	err := os.Remove("project")
	fmt.Println("Remove непустого:", err != nil)
	os.RemoveAll("project")
	_, err = os.Stat("project")
	fmt.Println("после RemoveAll:", errors.Is(err, fs.ErrNotExist))
}

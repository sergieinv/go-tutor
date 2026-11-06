// Пути к файлам собирают и разбирают пакетом `path/filepath`:
// он знает разделитель текущей ОС. Пакет `path` — для путей
// со слешами всегда: URL, пути внутри zip и `io/fs`.

package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
)

func main() {
	// `Join` склеивает элементы через разделитель ОС
	// и сразу нормализует результат (`Clean`).
	p := filepath.Join("data", "2024", "..", "reports", "q1.tar.gz")
	fmt.Println("Join:", p)

	// Разбор: каталог, последний элемент, расширение.
	// `Ext` берёт только часть после последней точки.
	fmt.Println("Dir:", filepath.Dir(p))
	fmt.Println("Base:", filepath.Base(p))
	fmt.Println("Ext:", filepath.Ext(p))

	// `Clean` убирает `.`, `..`, двойные и хвостовые слеши.
	fmt.Println("Clean:", filepath.Clean("a//b/./c/../d/"))

	// `Rel` — путь от одного каталога к другому.
	rel, _ := filepath.Rel("project/src", "project/docs/readme.md")
	fmt.Println("Rel:", rel)

	// `Abs` дописывает текущий каталог. Сам путь зависит
	// от машины, поэтому печатаем только проверку.
	abs, _ := filepath.Abs("data")
	fmt.Println("IsAbs:", filepath.IsAbs("data"), filepath.IsAbs(abs))

	// Разделитель — `/` в Linux и macOS, `\` в Windows.
	// `ToSlash`/`FromSlash` переводят между формами.
	fmt.Printf("Separator: %q\n", filepath.Separator)
	fmt.Println("FromSlash:", filepath.FromSlash("cfg/app.yaml"))

	// `IsLocal` проверяет, что путь не абсолютный и не
	// выходит наружу через `..`, — защита от path traversal.
	for _, name := range []string{"img/a.png", "../etc/passwd", "a/../../b"} {
		fmt.Printf("IsLocal(%q) = %v\n", name, filepath.IsLocal(name))
	}

	// `Match` сверяет имя с шаблоном. `*` не переходит
	// через разделитель: `*.go` не совпадает с `a/b.go`.
	m1, _ := filepath.Match("*.go", "main.go")
	m2, _ := filepath.Match("*.go", "cmd/main.go")
	fmt.Println("Match:", m1, m2)

	// `Glob` ищет файлы по шаблону на диске.
	os.MkdirAll("logs", 0o755)
	defer os.RemoveAll("logs")
	for _, n := range []string{"app.log", "db.log", "notes.txt"} {
		os.WriteFile(filepath.Join("logs", n), nil, 0o644)
	}
	logs, _ := filepath.Glob(filepath.Join("logs", "*.log"))
	fmt.Println("Glob:", logs)

	// Для URL — пакет `path`: в Windows `filepath.Join`
	// вставил бы сюда обратные слеши.
	fmt.Println("path.Join:", path.Join("/api/v1", "users", "42"))
}

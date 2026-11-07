// Код под тестом: текстовый отчёт, заголовок из переменной
// окружения и сохранение в файл. Всё это зависит от
// окружения — как раз то, что готовят фикстуры.

package report

import (
	"fmt"
	"os"
	"strings"
)

type Line struct {
	Name       string
	Qty, Price int
}

// Render строит отчёт: строка на позицию и итог.
func Render(title string, lines []Line) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", title)
	total := 0
	for _, l := range lines {
		sum := l.Qty * l.Price
		total += sum
		fmt.Fprintf(&b, "%-8s %3d x %5d = %6d\n", l.Name, l.Qty, l.Price, sum)
	}
	fmt.Fprintf(&b, "итого: %d\n", total)
	return b.String()
}

// Title берёт заголовок из REPORT_TITLE.
func Title() string {
	if t := os.Getenv("REPORT_TITLE"); t != "" {
		return t
	}
	return "Отчёт"
}

// Save пишет отчёт в файл в текущем каталоге.
func Save(name, text string) error {
	return os.WriteFile(name, []byte(text), 0o644)
}

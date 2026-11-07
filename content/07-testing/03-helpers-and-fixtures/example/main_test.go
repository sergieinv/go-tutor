package report

import (
	"flag"
	"fmt"
	"os"
	"testing"
)

// `TestMain` — точка входа тестового бинарника, если нужна
// подготовка на весь пакет (поднять контейнер, прогреть кеш).
// Без неё `go test` вызывает тесты сам. Флаги разбирает
// `m.Run`; если они нужны раньше — вызовите `flag.Parse()`.
func TestMain(m *testing.M) {
	fmt.Println("TestMain: подготовка пакета")
	code := m.Run()
	fmt.Println("TestMain: уборка")
	os.Exit(code)
}

// Флаг объявлен в тестовом файле: `go test -update`
// перезапишет эталоны вместо сравнения с ними.
var update = flag.Bool("update", false, "перезаписать golden-файлы")

// Хелпер начинается с `t.Helper()`: тогда в сообщениях
// будет строка вызывающего теста, а не строка хелпера.
// Golden-файлы принято класть в `testdata/` — его игнорирует
// `go build`; в песочнице подкаталогов нет, файл лежит рядом.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := name + ".golden.txt"
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden: %v (запустите с -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s: отличается от эталона\n got:\n%s\nwant:\n%s", path, got, want)
	}
	t.Logf("%s совпал с эталоном", path)
}

func TestRender(t *testing.T) {
	got := Render("Склад", []Line{{"гвозди", 100, 2}, {"молоток", 1, 450}})
	golden(t, "render", got) // в логе — эта строка, а не строка в golden
}

// `t.Setenv` меняет переменную на время теста и сам вернёт
// старое значение. `t.TempDir` — новый каталог, удалится
// после теста. `t.Chdir` (Go 1.24) — рабочий каталог на
// время теста. Все трое несовместимы с `t.Parallel`.
func TestSave(t *testing.T) {
	t.Setenv("REPORT_TITLE", "Склад")
	t.Chdir(t.TempDir())
	if err := Save("out.txt", Render(Title(), nil)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("out.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("в файле: %q", b)
}

// `t.Cleanup` выполняется после теста и всех его подтестов,
// в обратном порядке регистрации (LIFO). `t.Context` (Go
// 1.24) отменяется прямо перед запуском Cleanup-функций.
func TestCleanup(t *testing.T) {
	ctx := t.Context()
	t.Cleanup(func() { t.Logf("cleanup 1, ctx.Err() = %v", ctx.Err()) })
	t.Cleanup(func() { t.Log("cleanup 2 — выполнится первым") })
	t.Logf("тело теста, ctx.Err() = %v", ctx.Err())
}

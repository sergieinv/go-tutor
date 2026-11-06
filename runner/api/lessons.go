package api

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"gotutor/runner/sandbox"
)

// Lesson — то, что runner знает об уроке: флаги запуска и тесты задачи.
// Тесты берутся только из контента — клиент присылает лишь своё решение.
type Lesson struct {
	ID     string
	Flags  sandbox.Flags
	Tests  sandbox.Files   // task_test.go и hidden_test.go
	Hidden map[string]bool // имена тестов из hidden_test.go
	// Solution — эталон. Сайт показывает его после успеха или трёх попыток;
	// в статическую сборку он не попадает.
	Solution string
}

var dirName = regexp.MustCompile(`^\d{2}-[a-z0-9-]+$`)

// LoadLessons читает content/<module>/<lesson>/ так же, как сайт и lessonlint.
func LoadLessons(root string) (map[string]*Lesson, error) {
	modules, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	lessons := map[string]*Lesson{}
	for _, m := range modules {
		if !m.IsDir() || !dirName.MatchString(m.Name()) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, m.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || !dirName.MatchString(e.Name()) {
				continue
			}
			l, err := loadLesson(filepath.Join(root, m.Name(), e.Name()))
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", m.Name(), e.Name(), err)
			}
			l.ID = m.Name() + "/" + e.Name()
			lessons[l.ID] = l
		}
	}
	return lessons, nil
}

func loadLesson(dir string) (*Lesson, error) {
	l := &Lesson{Tests: sandbox.Files{}, Hidden: map[string]bool{}}
	src, err := os.ReadFile(filepath.Join(dir, "lesson.md"))
	if err != nil {
		return nil, err
	}
	if front, ok := frontmatter(src); ok {
		var meta struct {
			Runner sandbox.Flags `yaml:"runner"`
		}
		if err := yaml.Unmarshal(front, &meta); err != nil {
			return nil, err
		}
		l.Flags = meta.Runner
	}
	for _, name := range []string{"task_test.go", "hidden_test.go"} {
		b, err := os.ReadFile(filepath.Join(dir, "task", name))
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		l.Tests[name] = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "task", "solution.go")); err == nil {
		l.Solution = string(b)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if hidden, ok := l.Tests["hidden_test.go"]; ok {
		f, err := parser.ParseFile(token.NewFileSet(), "hidden_test.go", hidden, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				l.Hidden[fn.Name.Name] = true
			}
		}
	}
	return l, nil
}

func frontmatter(src []byte) ([]byte, bool) {
	rest, ok := bytes.CutPrefix(src, []byte("---\n"))
	if !ok {
		return nil, false
	}
	front, _, ok := bytes.Cut(rest, []byte("\n---\n"))
	return front, ok
}

// isHidden сообщает, относится ли тест (или подтест) к скрытым.
func (l *Lesson) isHidden(name string) bool {
	top, _, _ := strings.Cut(name, "/")
	return l.Hidden[top]
}

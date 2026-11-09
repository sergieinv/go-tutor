package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Набор запросов строится из того же контента, что отдаёт сайт: примеры
// уроков (example/), эталоны задач (task/solution.go) и ожидаемый вывод
// (example/output.txt). Так нагрузка повторяет реальные запросы ученика,
// а результат можно проверить на корректность.

var dirName = regexp.MustCompile(`^\d{2}-[a-z0-9-]+$`)

// fileName — имена файлов, которые принимает runner (sandbox.Files.Validate).
var fileName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*\.(go|mod|sum|proto|txt|json|sql|yaml|lock)$`)

// labsFlag — урок просит базы профиля labs. Такие уроки в нагрузку не
// берём: их вывод зависит от настоящей базы.
var labsFlag = regexp.MustCompile(`(?m)^\s*labs:\s*\[`)

// testDuration — время в выводе go test: от запуска к запуску разное.
var testDuration = regexp.MustCompile(`\(\d+\.\d+s\)`)

// lesson — урок, пригодный для нагрузки.
type lesson struct {
	id string
	// example — файлы примера (без output.txt), как их присылает сайт.
	example map[string]string
	// wantOutput — example/output.txt: с ним сверяется stdout.
	wantOutput string
	// hasTests — в примере есть _test.go: сайт запускает его через /api/gotest.
	hasTests bool
	// solution — task/solution.go: эталон для /api/test.
	solution string
	// concurrency — урок из модуля про конкурентность: для него есть смысл
	// гонять /api/trace (живая визуализация).
	concurrency bool
}

// loadLessons читает уроки из каталога контента.
func loadLessons(root string) ([]*lesson, error) {
	modules, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []*lesson
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
			l, err := loadLesson(filepath.Join(root, m.Name(), e.Name()), m.Name()+"/"+e.Name())
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", m.Name(), e.Name(), err)
			}
			if l != nil {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("в %s нет уроков с примерами", root)
	}
	return out, nil
}

func loadLesson(dir, id string) (*lesson, error) {
	md, err := os.ReadFile(filepath.Join(dir, "lesson.md"))
	if err != nil {
		return nil, err
	}
	if labsFlag.Match(md) {
		return nil, nil // нужна настоящая база — в нагрузку не берём
	}
	l := &lesson{id: id, example: map[string]string{}, concurrency: strings.HasPrefix(id, "05-")}
	entries, err := os.ReadDir(filepath.Join(dir, "example"))
	if err != nil {
		return nil, nil // урок без примера (теория, квиз)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "output.txt" || !fileName.MatchString(name) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "example", name))
		if err != nil {
			return nil, err
		}
		l.example[name] = string(b)
		if strings.HasSuffix(name, "_test.go") {
			l.hasTests = true
		}
	}
	if _, ok := l.example["main.go"]; !ok {
		return nil, nil
	}
	if b, err := os.ReadFile(filepath.Join(dir, "example", "output.txt")); err == nil {
		l.wantOutput = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "task", "solution.go")); err == nil {
		l.solution = string(b)
	}
	return l, nil
}

// normalize приводит вывод к виду, в котором его сравнивает lessonlint:
// без хвостовых пробелов и перевода строки в конце.
func normalize(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// maskDurations убирает из вывода go test время тестов.
func maskDurations(s string) string { return testDuration.ReplaceAllString(s, "(0.00s)") }

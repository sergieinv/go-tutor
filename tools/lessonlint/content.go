package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"gotutor/runner/sandbox"
)

var dirName = regexp.MustCompile(`^\d{2}-[a-z0-9-]+$`)

type Module struct {
	Dir     string    `yaml:"-"`
	Slug    string    `yaml:"-"`
	Title   string    `yaml:"title"`
	Summary string    `yaml:"summary"`
	Goals   []string  `yaml:"goals"`
	Lessons []*Lesson `yaml:"-"`
}

type Lesson struct {
	Dir    string
	Module *Module
	Slug   string
	Meta   Frontmatter
	// Sections — заголовки второго уровня из тела lesson.md.
	Sections []string
	Quiz     Quiz
	HasTask  bool
	// HasLive — есть live/main.go для живой визуализации.
	HasLive bool
}

// ID — идентификатор урока вида "02-basics/05-slices".
func (l *Lesson) ID() string { return l.Module.Slug + "/" + l.Slug }

type Frontmatter struct {
	Title         string   `yaml:"title"`
	Summary       string   `yaml:"summary"`
	Difficulty    int      `yaml:"difficulty"`
	Minutes       int      `yaml:"minutes"`
	Prerequisites []string `yaml:"prerequisites"`
	Docs          []struct {
		Title string `yaml:"title"`
		URL   string `yaml:"url"`
	} `yaml:"docs"`
	Runner sandbox.Flags `yaml:"runner"`
	// TaskKind — "fix" для задач «найди и почини»: шаблон — рабочий, но
	// уязвимый код. Функциональные тесты на нём проходят, эксплойты
	// (TestExploit… в hidden_test.go) — срабатывают. "review" — «приведи
	// к виду для ревью»: шаблон работает, но падают проверки TestReview…
	// (идиомы, ошибки, context, ресурсы) по чек-листу ревью.
	TaskKind string `yaml:"taskKind"`
	// Buf — проверка контракта примера: ExpectLint — правила buf lint,
	// которые пример нарушает намеренно (пусто — контракт чистый).
	Buf *struct {
		ExpectLint []string `yaml:"expectLint"`
	} `yaml:"buf"`
	// Govulncheck — песочница показывает кнопку govulncheck для примера.
	Govulncheck *struct {
		// Expect — id уязвимостей (GO-ГГГГ-NNNN), которые govulncheck
		// обязан найти в вызовах примера.
		Expect []string `yaml:"expect"`
	} `yaml:"govulncheck"`
}

type Quiz struct {
	Questions []Question `yaml:"questions"`
}

type Question struct {
	ID       string   `yaml:"id"`
	Kind     string   `yaml:"kind"`
	Prompt   string   `yaml:"prompt"`
	Code     string   `yaml:"code"`
	Options  []Option `yaml:"options"`
	Compiles *bool    `yaml:"compiles"`
	Explain  string   `yaml:"explain"`
}

type Option struct {
	Text    string `yaml:"text"`
	Correct bool   `yaml:"correct"`
	Explain string `yaml:"explain"`
}

// Problems собирает ошибки и предупреждения одного урока или модуля.
type Problems struct {
	Errors   []string
	Warnings []string
}

func (p *Problems) Errorf(format string, args ...any) {
	p.Errors = append(p.Errors, fmt.Sprintf(format, args...))
}

func (p *Problems) Warnf(format string, args ...any) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...))
}

// LoadModules читает все модули и уроки из каталога контента.
// Структурные ошибки складываются в problems по ключу пути.
// При update отсутствующий example/output.txt не считается ошибкой — его создаст проверка.
func LoadModules(root string, update bool, problems map[string]*Problems) ([]*Module, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var modules []*Module
	for _, e := range entries {
		if !e.IsDir() || !dirName.MatchString(e.Name()) {
			continue
		}
		p := &Problems{}
		problems[e.Name()] = p
		m := &Module{Dir: filepath.Join(root, e.Name()), Slug: e.Name()}
		if err := readYAMLStrict(filepath.Join(m.Dir, "module.yaml"), m); err != nil {
			p.Errorf("module.yaml: %v", err)
		} else if m.Title == "" {
			p.Errorf("module.yaml: пустое поле title")
		}
		lessonDirs, err := os.ReadDir(m.Dir)
		if err != nil {
			return nil, err
		}
		for _, le := range lessonDirs {
			if !le.IsDir() {
				continue
			}
			l := &Lesson{Dir: filepath.Join(m.Dir, le.Name()), Module: m, Slug: le.Name()}
			lp := &Problems{}
			problems[l.ID()] = lp
			if !dirName.MatchString(le.Name()) {
				lp.Errorf("имя папки урока должно иметь вид NN-slug")
				continue
			}
			loadLesson(l, update, lp)
			m.Lessons = append(m.Lessons, l)
		}
		modules = append(modules, m)
	}
	checkPrerequisites(modules, problems)
	checkSlides(root, modules, problems)
	return modules, nil
}

func loadLesson(l *Lesson, update bool, p *Problems) {
	src, err := os.ReadFile(filepath.Join(l.Dir, "lesson.md"))
	if err != nil {
		p.Errorf("lesson.md: %v", err)
		return
	}
	front, body, err := splitFrontmatter(src)
	if err != nil {
		p.Errorf("lesson.md: %v", err)
		return
	}
	if err := decodeStrict(front, &l.Meta); err != nil {
		p.Errorf("lesson.md frontmatter: %v", err)
	}
	m := l.Meta
	if m.Title == "" {
		p.Errorf("frontmatter: пустое поле title")
	}
	if m.Summary == "" {
		p.Errorf("frontmatter: пустое поле summary")
	}
	if m.Difficulty < 1 || m.Difficulty > 5 {
		p.Errorf("frontmatter: difficulty должен быть от 1 до 5, сейчас %d", m.Difficulty)
	}
	if m.Minutes <= 0 {
		p.Errorf("frontmatter: minutes должен быть положительным")
	}
	if m.TaskKind != "" && m.TaskKind != "fix" && m.TaskKind != "review" {
		p.Errorf("frontmatter: taskKind может быть только fix или review, сейчас %q", m.TaskKind)
	}
	if err := sandbox.ValidLabs(m.Runner.Labs); err != nil {
		p.Errorf("frontmatter: runner.labs: %v", err)
	}
	for _, d := range m.Docs {
		if d.Title == "" || !strings.HasPrefix(d.URL, "https://") {
			p.Errorf("frontmatter: ссылка в docs должна иметь title и https-url: %+v", d)
		}
	}

	l.Sections = h2Sections(body)
	for _, s := range []string{"Как это в C#", "Ловушки"} {
		if !slices.Contains(l.Sections, s) {
			p.Errorf("lesson.md: нет раздела «## %s»", s)
		}
	}
	known := []string{"Как это в C#", "Под капотом", "Ловушки", "Задание"}
	for _, s := range l.Sections {
		if !slices.Contains(known, s) {
			p.Errorf("lesson.md: неизвестный раздел «## %s», допустимы: %s", s, strings.Join(known, ", "))
		}
	}

	requireFile(l, p, "example/main.go")
	if !update {
		requireFile(l, p, "example/output.txt")
	}
	if cs, _ := filepath.Glob(filepath.Join(l.Dir, "csharp", "*.cs")); len(cs) == 0 {
		p.Errorf("нет файла csharp/*.cs для блока «Как это в C#»")
	}

	if err := readYAMLStrict(filepath.Join(l.Dir, "quiz.yaml"), &l.Quiz); err != nil {
		p.Errorf("quiz.yaml: %v", err)
	} else {
		validateQuiz(l.Quiz, p)
	}

	// viz.ts проверяют тесты сайта (npm test), здесь — только его связь с уроком.
	_, vizErr := os.Stat(filepath.Join(l.Dir, "viz.ts"))
	if vizErr == nil && !slices.Contains(l.Sections, "Под капотом") {
		p.Errorf("lesson.md: есть viz.ts, но нет раздела «## Под капотом», где он показывается")
	}
	// viz-live.ts — живой визуализатор в дополнение к сценарному viz.ts.
	_, liveVizErr := os.Stat(filepath.Join(l.Dir, "viz-live.ts"))
	if liveVizErr == nil && vizErr != nil {
		p.Errorf("есть viz-live.ts без viz.ts: единственный визуализатор урока лежит в viz.ts")
	}
	if _, err := os.Stat(filepath.Join(l.Dir, "live")); err == nil {
		l.HasLive = true
		requireFile(l, p, "live/main.go")
		if vizErr != nil {
			p.Errorf("есть папка live/, но нет viz.ts, который её показывает")
		}
	} else if liveVizErr == nil {
		p.Errorf("есть viz-live.ts, но нет live/main.go")
	}

	if _, err := os.Stat(filepath.Join(l.Dir, "task")); err != nil && l.Meta.TaskKind != "" {
		p.Errorf("frontmatter: taskKind без папки task/")
	} else if err == nil {
		l.HasTask = true
		for _, f := range []string{"task.go", "task_test.go", "hidden_test.go", "solution.go", "hints.md"} {
			requireFile(l, p, "task/"+f)
		}
		if !slices.Contains(l.Sections, "Задание") {
			p.Errorf("lesson.md: есть папка task/, но нет раздела «## Задание»")
		}
		if hints, err := os.ReadFile(filepath.Join(l.Dir, "task", "hints.md")); err == nil {
			if n := len(h2Sections(hints)); n != 3 {
				p.Errorf("task/hints.md: нужно ровно 3 подсказки (## Подсказка N), найдено %d", n)
			}
		}
	}
}

func validateQuiz(q Quiz, p *Problems) {
	if n := len(q.Questions); n < 3 || n > 5 {
		p.Errorf("quiz.yaml: нужно 3–5 вопросов, сейчас %d", n)
	}
	seen := map[string]bool{}
	for i, qu := range q.Questions {
		where := fmt.Sprintf("quiz.yaml: вопрос %d (%s)", i+1, qu.ID)
		if qu.ID == "" {
			p.Errorf("%s: пустой id", where)
		} else if seen[qu.ID] {
			p.Errorf("%s: id повторяется", where)
		}
		seen[qu.ID] = true
		if qu.Prompt == "" {
			p.Errorf("%s: пустой prompt", where)
		}
		switch qu.Kind {
		case "output", "compile":
			if !strings.HasPrefix(strings.TrimSpace(qu.Code), "package ") {
				p.Errorf("%s: code должен быть полной программой, начинающейся с package", where)
			}
		case "single", "multiple":
		default:
			p.Errorf("%s: неизвестный kind %q", where, qu.Kind)
			continue
		}
		if qu.Kind == "compile" {
			if qu.Compiles == nil {
				p.Errorf("%s: для kind compile нужно поле compiles", where)
			}
			if qu.Explain == "" {
				p.Errorf("%s: для kind compile нужно поле explain", where)
			}
			if len(qu.Options) > 0 {
				p.Errorf("%s: у kind compile варианты строятся автоматически, уберите options", where)
			}
			continue
		}
		correct := 0
		for _, o := range qu.Options {
			if o.Correct {
				correct++
			}
			if o.Explain == "" {
				p.Errorf("%s: у варианта %q нет explain", where, o.Text)
			}
		}
		switch {
		case len(qu.Options) < 2:
			p.Errorf("%s: нужно хотя бы 2 варианта", where)
		case qu.Kind == "multiple" && correct < 1:
			p.Errorf("%s: нет правильных вариантов", where)
		case qu.Kind != "multiple" && correct != 1:
			p.Errorf("%s: должен быть ровно один правильный вариант, сейчас %d", where, correct)
		}
	}
}

func checkPrerequisites(modules []*Module, problems map[string]*Problems) {
	ids := map[string]bool{}
	for _, m := range modules {
		for _, l := range m.Lessons {
			ids[l.ID()] = true
		}
	}
	for _, m := range modules {
		for _, l := range m.Lessons {
			for _, pre := range l.Meta.Prerequisites {
				if !ids[pre] {
					// Пока курс наполняется, ссылка на ещё не написанный урок — не ошибка.
					problems[l.ID()].Warnf("пререквизит %s ещё не существует", pre)
				}
			}
		}
	}
}

var vizDirective = regexp.MustCompile(`(?m)^@viz\s+(\S+)\s*$`)

// checkSlides проверяет slides.md модулей: каждый @viz ссылается на урок
// с viz.ts, на одном слайде не больше одного визуализатора.
func checkSlides(root string, modules []*Module, problems map[string]*Problems) {
	hasViz := map[string]bool{}
	for _, m := range modules {
		for _, l := range m.Lessons {
			if _, err := os.Stat(filepath.Join(l.Dir, "viz.ts")); err == nil {
				hasViz[l.ID()] = true
			}
		}
	}
	for _, m := range modules {
		src, err := os.ReadFile(filepath.Join(root, m.Slug, "slides.md"))
		if err != nil {
			continue
		}
		p := problems[m.Slug]
		slides := regexp.MustCompile(`(?m)^---$`).Split(string(src), -1)
		for i, s := range slides {
			refs := vizDirective.FindAllStringSubmatch(s, -1)
			if len(refs) > 1 {
				p.Errorf("slides.md, слайд %d: больше одного @viz", i+1)
			}
			for _, r := range refs {
				if !hasViz[r[1]] {
					p.Errorf("slides.md, слайд %d: у урока %s нет viz.ts", i+1, r[1])
				}
			}
		}
	}
}

func requireFile(l *Lesson, p *Problems, rel string) {
	if _, err := os.Stat(filepath.Join(l.Dir, rel)); err != nil {
		p.Errorf("нет файла %s", rel)
	}
}

func splitFrontmatter(src []byte) (front, body []byte, err error) {
	const delim = "---\n"
	if !bytes.HasPrefix(src, []byte(delim)) {
		return nil, nil, errors.New("файл должен начинаться с frontmatter (---)")
	}
	rest := src[len(delim):]
	end := bytes.Index(rest, []byte("\n"+delim))
	if end < 0 {
		return nil, nil, errors.New("не найден конец frontmatter (---)")
	}
	return rest[:end+1], rest[end+1+len(delim):], nil
}

// h2Sections возвращает заголовки второго уровня вне блоков кода.
func h2Sections(md []byte) []string {
	var out []string
	inFence := false
	sc := bufio.NewScanner(bytes.NewReader(md))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			out = append(out, strings.TrimSpace(line[3:]))
		}
	}
	return out
}

func readYAMLStrict(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeStrict(data, v)
}

func decodeStrict(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(v)
}

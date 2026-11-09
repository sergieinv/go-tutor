package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// task — один запрос нагрузки: куда, с каким телом и как проверить ответ.
type task struct {
	// label — как запрос попадёт в отчёт («run», «test», «abuse:loop»…).
	label string
	// lesson — урок, из которого взят запрос: с ним ошибку видно, где искать.
	lesson string
	path   string
	body   []byte
	// check проверяет ответ. Ошибка — находка нагрузочного теста, а не
	// ошибка клиента: она попадает в отчёт по видам.
	check func(status int, r *apiResp) error
}

// apiResp — поля ответов runner, которые нужны для проверки.
type apiResp struct {
	Error       string `json:"error"`
	BuildFailed bool   `json:"buildFailed"`
	BuildOutput string `json:"buildOutput"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	ExitCode    int    `json:"exitCode"`
	TimedOut    bool   `json:"timedOut"`
	Truncated   bool   `json:"truncated"`
	DurationMs  int64  `json:"durationMs"`
	Passed      bool   `json:"passed"`
	Tests       []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"tests"`
	TraceError string `json:"traceError"`
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// normalTasks — запросы, которые делает ученик, читая урок: запуск примера,
// проверка эталонного решения, go test примера с тестами, живая трасса.
func normalTasks(lessons []*lesson, verify bool) []task {
	var out []task
	for _, l := range lessons {
		want := l.wantOutput
		if l.hasTests {
			// Пример с тестами сайт запускает через /api/gotest.
			out = append(out, task{
				label: "gotest", lesson: l.id,
				path: "/api/gotest",
				body: mustJSON(map[string]any{"lesson": l.id, "files": l.example, "mode": "test"}),
				check: func(status int, r *apiResp) error {
					if err := runOK(status, r); err != nil {
						return err
					}
					if verify && want != "" && normalize(maskDurations(r.Stdout)) != normalize(want) {
						return fmt.Errorf("вывод не совпал с output.txt")
					}
					return nil
				},
			})
		} else {
			out = append(out, task{
				label: "run", lesson: l.id,
				path: "/api/run",
				body: mustJSON(map[string]any{"lesson": l.id, "files": l.example}),
				check: func(status int, r *apiResp) error {
					if err := runOK(status, r); err != nil {
						return err
					}
					if verify && want != "" && normalize(r.Stdout) != normalize(want) {
						return fmt.Errorf("вывод не совпал с output.txt")
					}
					return nil
				},
			})
		}
		if l.solution != "" {
			out = append(out, task{
				label: "test", lesson: l.id,
				path: "/api/test",
				body: mustJSON(map[string]any{"lesson": l.id, "files": map[string]string{"task.go": l.solution}}),
				check: func(status int, r *apiResp) error {
					if status != 200 {
						return fmt.Errorf("HTTP %d: %s", status, short(r.Error))
					}
					if r.BuildFailed {
						return fmt.Errorf("эталон не собрался: %s", short(r.BuildOutput))
					}
					if !r.Passed {
						return fmt.Errorf("эталон не прошёл тесты")
					}
					return nil
				},
			})
		}
		if l.concurrency {
			out = append(out, task{
				label: "trace", lesson: l.id,
				path: "/api/trace",
				body: mustJSON(map[string]any{"lesson": l.id, "files": l.example}),
				check: func(status int, r *apiResp) error {
					if status != 200 {
						return fmt.Errorf("HTTP %d: %s", status, short(r.Error))
					}
					if r.BuildFailed {
						return fmt.Errorf("не собралось: %s", short(r.BuildOutput))
					}
					return nil
				},
			})
		}
	}
	return out
}

// runOK — обычная проверка запуска примера: ответ 200, собралось,
// уложилось в лимиты, вывод не обрезан.
func runOK(status int, r *apiResp) error {
	switch {
	case status != 200:
		return fmt.Errorf("HTTP %d: %s", status, short(r.Error))
	case r.BuildFailed:
		return fmt.Errorf("не собралось: %s", short(r.BuildOutput))
	case r.TimedOut:
		return fmt.Errorf("таймаут запуска")
	case r.Truncated:
		return fmt.Errorf("вывод обрезан")
	case r.ExitCode != 0:
		return fmt.Errorf("exit=%d: %s", r.ExitCode, short(r.Stderr))
	}
	return nil
}

// program — программа одного файла для «злых» запросов.
func program(body string, imports ...string) map[string]string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "import %q\n", imp)
	}
	fmt.Fprintf(&b, "\nfunc main() {\n%s\n}\n", body)
	return map[string]string{"main.go": b.String()}
}

// contained — «злой» запрос обслужен и удержан лимитами: runner ответил 200
// (или честным 503, когда слоты заняты), а программа убита по таймауту,
// лимиту вывода или памяти.
func contained(status int, r *apiResp) error {
	if status == 503 {
		return nil // очередь слотов — не отказ сервиса
	}
	if status != 200 {
		return fmt.Errorf("HTTP %d: %s", status, short(r.Error))
	}
	if r.BuildFailed {
		return fmt.Errorf("не собралось: %s", short(r.BuildOutput))
	}
	if !r.TimedOut && !r.Truncated && r.ExitCode == 0 {
		return fmt.Errorf("программа завершилась успешно — лимит не сработал")
	}
	return nil
}

// abuseTasks — запросы злоупотребления: ученик (или присланный по ссылке
// «Поделиться» код) пытается занять слот навсегда, съесть память, расплодить
// процессы или залить вывод.
func abuseTasks() []task {
	mk := func(label string, files map[string]string) task {
		return task{label: "abuse:" + label, path: "/api/run",
			body: mustJSON(map[string]any{"files": files}), check: contained}
	}
	return []task{
		mk("loop", program("for {\n}")),
		mk("forkbomb", program("for {\n\t\tcmd := exec.Command(os.Args[0])\n\t\t_ = cmd.Start()\n\t}", "os", "os/exec")),
		mk("memory", program(`var keep [][]byte
	for i := 0; i < 64; i++ {
		b := make([]byte, 64<<20)
		for j := range b {
			b[j] = 1
		}
		keep = append(keep, b)
	}
	fmt.Println(len(keep))`, "fmt")),
		mk("output", program(`line := strings.Repeat("x", 1024)
	for {
		fmt.Println(line)
	}`, "fmt", "strings")),
		mk("goroutines", program(`var wg sync.WaitGroup
	for i := 0; i < 1<<20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); select {} }()
	}
	wg.Wait()`, "sync")),
		mk("tmpfs", program(`for i := 0; ; i++ {
		name := fmt.Sprintf("fill-%d.bin", i)
		if err := os.WriteFile(name, make([]byte, 8<<20), 0o644); err != nil {
			fmt.Println("stop:", err)
			return
		}
	}`, "fmt", "os")),
	}
}

func short(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

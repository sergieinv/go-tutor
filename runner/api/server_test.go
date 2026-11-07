package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotutor/runner/livetrace"
	"gotutor/runner/sandbox"
)

const contentDir = "../../content"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	exec, err := sandbox.New(sandbox.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	lessons, err := LoadLessons(contentDir)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(exec, lessons, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, srv *httptest.Server, path string, body any, out any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func readLessonFile(t *testing.T, lesson, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(contentDir, lesson, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunExample(t *testing.T) {
	srv := newTestServer(t)
	const lesson = "02-basics/05-slices"
	var res sandbox.RunResult
	code := post(t, srv, "/api/run", map[string]any{
		"lesson": lesson,
		"files":  map[string]string{"main.go": readLessonFile(t, lesson, "example/main.go")},
	}, &res)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if want := readLessonFile(t, lesson, "example/output.txt"); res.Stdout != want {
		t.Errorf("stdout = %q, want %q", res.Stdout, want)
	}
}

func TestTaskSolutionAndTemplate(t *testing.T) {
	srv := newTestServer(t)
	for _, lesson := range []string{"02-basics/05-slices", "05-concurrency/07-select"} {
		t.Run(lesson, func(t *testing.T) {
			var res sandbox.TestResult
			post(t, srv, "/api/test", map[string]any{
				"lesson": lesson,
				"files":  map[string]string{"task.go": readLessonFile(t, lesson, "task/solution.go")},
			}, &res)
			if !res.Passed {
				t.Fatalf("эталон не прошёл: %+v", res)
			}
			hidden := 0
			for _, tc := range res.Tests {
				if tc.Hidden {
					hidden++
				}
			}
			if hidden == 0 {
				t.Errorf("скрытые тесты не помечены: %+v", res.Tests)
			}

			res = sandbox.TestResult{}
			post(t, srv, "/api/test", map[string]any{
				"lesson": lesson,
				"files":  map[string]string{"task.go": readLessonFile(t, lesson, "task/task.go")},
			}, &res)
			if res.Passed || res.BuildFailed {
				t.Fatalf("шаблон должен компилироваться и падать: %+v", res)
			}
		})
	}
}

func TestTestRejectsClientTests(t *testing.T) {
	srv := newTestServer(t)
	var res map[string]string
	code := post(t, srv, "/api/test", map[string]any{
		"lesson": "02-basics/05-slices",
		"files":  map[string]string{"task_test.go": "package task"},
	}, &res)
	if code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %v", code, res)
	}
}

func TestFmt(t *testing.T) {
	srv := newTestServer(t)
	var res fmtResponse
	post(t, srv, "/api/fmt", map[string]any{
		"files": map[string]string{"main.go": "package main\nfunc main(){\nx:=1\n_=x}\n", "bad.go": "package main\nfunc {"},
	}, &res)
	if want := "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"; res.Files["main.go"] != want {
		t.Errorf("fmt = %q, want %q", res.Files["main.go"], want)
	}
	if res.Errors["bad.go"] == "" {
		t.Errorf("нет ошибки для bad.go: %+v", res)
	}
}

func TestSolution(t *testing.T) {
	srv := newTestServer(t)
	var res struct {
		Files map[string]string `json:"files"`
	}
	code := post(t, srv, "/api/solution", map[string]any{"lesson": "02-basics/05-slices"}, &res)
	if code != http.StatusOK || res.Files["task.go"] != readLessonFile(t, "02-basics/05-slices", "task/solution.go") {
		t.Fatalf("status %d, files %v", code, res.Files)
	}
	var e map[string]string
	if code := post(t, srv, "/api/solution", map[string]any{"lesson": "nope"}, &e); code != http.StatusNotFound {
		t.Errorf("неизвестный урок: status %d", code)
	}
}

func TestTraceLive(t *testing.T) {
	srv := newTestServer(t)
	const lesson = "05-concurrency/07-select"
	var res livetrace.Result
	code := post(t, srv, "/api/trace", map[string]any{
		"lesson": lesson,
		"files":  map[string]string{"main.go": readLessonFile(t, lesson, "live/main.go")},
		"flags":  map[string]any{"gomaxprocs": 1},
	}, &res)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if res.Trace == nil {
		t.Fatalf("нет трассы: %s\n%s", res.TraceError, res.Stderr)
	}
	if len(res.Trace.Selects) != 3 || res.Trace.Total == 0 {
		t.Errorf("selects=%d events=%d", len(res.Trace.Selects), res.Trace.Total)
	}
	var msg map[string]string
	if code := post(t, srv, "/api/trace", map[string]any{"files": map[string]string{"lib.go": "package lib\n"}}, &msg); code != http.StatusBadRequest {
		t.Errorf("без main: status %d, %v", code, msg)
	}
}

func TestGoTest(t *testing.T) {
	srv := newTestServer(t)
	files := map[string]string{
		"main.go":      "package main\n\nfunc Double(x int) int { return x * 2 }\n\nfunc main() {}\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
	}
	var res sandbox.GoTestResult
	if code := post(t, srv, "/api/gotest", map[string]any{"files": files, "mode": "cover"}, &res); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "--- PASS: TestDouble") || !strings.Contains(res.Coverage, "Double") {
		t.Errorf("res = %+v", res)
	}
	var msg map[string]string
	if code := post(t, srv, "/api/gotest", map[string]any{"files": files, "mode": "fuzz", "fuzz": "-x"}, &msg); code != http.StatusBadRequest {
		t.Errorf("плохая цель fuzz: status %d, %v", code, msg)
	}
}

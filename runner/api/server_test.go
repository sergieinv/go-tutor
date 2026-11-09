package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// Флаги компилятора и GOTRACEBACK — только из белого списка.
func TestRunCompilerFlags(t *testing.T) {
	srv := newTestServer(t)
	files := map[string]string{"main.go": "package main\n\nfunc f() *int { x := 1; return &x }\n\nfunc main() { println(*f()) }\n"}
	var res sandbox.RunResult
	if code := post(t, srv, "/api/run", map[string]any{"files": files, "flags": map[string]any{"gcflags": "-m"}}, &res); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if !strings.Contains(res.CompilerOutput, "moved to heap: x") {
		t.Errorf("compilerOutput = %q", res.CompilerOutput)
	}
	for _, flags := range []map[string]any{{"gcflags": "-S"}, {"gcflags": "all=-m"}, {"gotraceback": "crash"}} {
		var msg map[string]string
		if code := post(t, srv, "/api/run", map[string]any{"files": files, "flags": flags}, &msg); code != http.StatusBadRequest {
			t.Errorf("%v: status %d, %v", flags, code, msg)
		}
	}
	var msg map[string]string
	if code := post(t, srv, "/api/pprof", map[string]any{"files": files}, &msg); code != http.StatusNotImplemented {
		t.Errorf("pprof без бинарника: status %d, %v", code, msg)
	}
}

// Без govulncheck в конфигурации /api/vulncheck честно отвечает 501.
func TestVulncheckNotConfigured(t *testing.T) {
	srv := newTestServer(t)
	var out map[string]string
	code := post(t, srv, "/api/vulncheck", map[string]any{"files": sandbox.Files{"main.go": "package main\n\nfunc main() {}\n"}}, &out)
	if code != http.StatusNotImplemented || out["error"] == "" {
		t.Fatalf("code = %d, out = %v", code, out)
	}
}

// /api/health отдаёт версию курса из GOTUTOR_VERSION (build-arg образа),
// без переменной — dev.
func TestHealthVersion(t *testing.T) {
	for env, want := range map[string]string{"1.2.3": "1.2.3", "": "dev"} {
		t.Setenv("GOTUTOR_VERSION", env)
		s := NewServer(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		var got struct {
			OK      bool   `json:"ok"`
			Version string `json:"version"`
		}
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || !got.OK || got.Version != want {
			t.Errorf("GOTUTOR_VERSION=%q: код %d, ответ %+v, ожидалась версия %q", env, w.Code, got, want)
		}
	}
}

// Чужая страница не должна запускать код: ни «простым» запросом без
// preflight (text/plain), ни запросом с Sec-Fetch-Site: cross-site.
// Временные ошибки песочницы — 503 с предложением повторить, а не 500:
// место в общем tmpfs рабочих каталогов мог занять другой запуск.
func TestFailStatuses(t *testing.T) {
	s := NewServer(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"слоты заняты":    {sandbox.ErrBusy, http.StatusServiceUnavailable},
		"кончилось место": {fmt.Errorf("%w: write main.go", sandbox.ErrNoSpace), http.StatusServiceUnavailable},
		"плохой ввод":     {&sandbox.InputError{Err: errors.New("недопустимое имя файла")}, http.StatusBadRequest},
		"прочее":          {errors.New("сломалось"), http.StatusInternalServerError},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/run", nil)
		if !s.fail(w, r, tc.err) {
			t.Errorf("%s: fail вернул false", name)
			continue
		}
		if w.Code != tc.want {
			t.Errorf("%s: код %d, ожидался %d", name, w.Code, tc.want)
		}
	}
}

func TestRejectCrossSite(t *testing.T) {
	srv := newTestServer(t)
	body := `{"files":{"main.go":"package main\nfunc main(){}\n"}}`
	cases := []struct {
		name   string
		header map[string]string
		want   int
	}{
		{"text/plain", map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"без Content-Type", nil, http.StatusUnsupportedMediaType},
		{"cross-site", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"чужой Origin", map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"}, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", srv.URL+"/api/fmt", strings.NewReader(body))
			for k, v := range c.header {
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

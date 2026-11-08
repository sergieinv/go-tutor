// Package api — HTTP-интерфейс runner: /api/run, /api/test, /api/gotest,
// /api/vulncheck, /api/fmt, /api/trace, /api/race.
package api

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"gotutor/runner/livetrace"
	"gotutor/runner/sandbox"
)

type Server struct {
	exec    *sandbox.Executor
	lessons map[string]*Lesson
	log     *slog.Logger
	// queueTimeout — сколько запрос ждёт свободный слот, прежде чем получить 503.
	queueTimeout time.Duration
	cache        *lru
}

func NewServer(exec *sandbox.Executor, lessons map[string]*Lesson, log *slog.Logger) *Server {
	return &Server{exec: exec, lessons: lessons, log: log, queueTimeout: 30 * time.Second, cache: newLRU(512)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/run", s.handleRun)
	mux.HandleFunc("POST /api/test", s.handleTest)
	mux.HandleFunc("POST /api/gotest", s.handleGoTest)
	mux.HandleFunc("POST /api/vulncheck", s.handleVulncheck)
	mux.HandleFunc("POST /api/fmt", s.handleFmt)
	mux.HandleFunc("POST /api/solution", s.handleSolution)
	mux.HandleFunc("POST /api/trace", s.handleTrace)
	mux.HandleFunc("POST /api/race", s.handleRace)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "lessons": len(s.lessons)})
	})
	return mux
}

type runRequest struct {
	Lesson string        `json:"lesson"`
	Files  sandbox.Files `json:"files"`
	Flags  sandbox.Flags `json:"flags"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if !decode(w, r, &req) {
		return
	}
	flags, ok := s.runFlags(w, req)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.queueTimeout)
	defer cancel()
	// /run не кешируется: у программ с горутинами, select и map вывод
	// от запуска к запуску разный, и ученик должен это видеть.
	res, err := s.exec.Run(ctx, req.Files, flags)
	if s.fail(w, r, err) {
		return
	}
	s.log.Info("run", "lesson", req.Lesson, "ms", res.DurationMs, "exit", res.ExitCode,
		"buildFailed", res.BuildFailed, "timedOut", res.TimedOut)
	writeJSON(w, http.StatusOK, res)
}

// runFlags — флаги урока с наложенными флагами запроса.
func (s *Server) runFlags(w http.ResponseWriter, req runRequest) (sandbox.Flags, bool) {
	flags := req.Flags
	if req.Lesson != "" {
		l, ok := s.lessons[req.Lesson]
		if !ok {
			writeError(w, http.StatusNotFound, "урок %q не найден", req.Lesson)
			return flags, false
		}
		flags = merge(l.Flags, req.Flags)
	}
	if err := validateFlags(flags); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return flags, false
	}
	return flags, true
}

// handleTrace запускает программу с runtime/trace для живой визуализации.
// Флаги — как у /run, кроме -race: он искажает тайминги.
func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if !decode(w, r, &req) {
		return
	}
	flags, ok := s.runFlags(w, req)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.queueTimeout)
	defer cancel()
	res, err := livetrace.Run(ctx, s.exec, req.Files, flags)
	if s.fail(w, r, err) {
		return
	}
	events := 0
	if res.Trace != nil {
		events = res.Trace.Total
	}
	s.log.Info("trace", "lesson", req.Lesson, "ms", res.DurationMs, "exit", res.ExitCode,
		"buildFailed", res.BuildFailed, "events", events, "traceError", res.TraceError)
	writeJSON(w, http.StatusOK, res)
}

// handleRace запускает программу с -race и отдаёт разобранные отчёты детектора
// для живого визуализатора гонок.
func (s *Server) handleRace(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if !decode(w, r, &req) {
		return
	}
	flags, ok := s.runFlags(w, req)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.queueTimeout)
	defer cancel()
	res, err := livetrace.RunRace(ctx, s.exec, req.Files, flags)
	if s.fail(w, r, err) {
		return
	}
	s.log.Info("race", "lesson", req.Lesson, "ms", res.DurationMs, "exit", res.ExitCode,
		"buildFailed", res.BuildFailed, "races", len(res.Races))
	writeJSON(w, http.StatusOK, res)
}

type goTestRequest struct {
	runRequest
	Mode string `json:"mode"`
	Fuzz string `json:"fuzz,omitempty"`
}

// handleGoTest запускает go test для примера с тестами: тесты, бенчмарки,
// fuzzing или покрытие. Как и /run, не кешируется — замеры и находки фаззера
// от запуска к запуску разные.
func (s *Server) handleGoTest(w http.ResponseWriter, r *http.Request) {
	var req goTestRequest
	if !decode(w, r, &req) {
		return
	}
	flags, ok := s.runFlags(w, req.runRequest)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.queueTimeout)
	defer cancel()
	res, err := s.exec.GoTest(ctx, req.Files, flags, sandbox.TestOptions{Mode: req.Mode, Fuzz: req.Fuzz})
	if s.fail(w, r, err) {
		return
	}
	s.log.Info("gotest", "lesson", req.Lesson, "mode", req.Mode, "ms", res.DurationMs, "exit", res.ExitCode,
		"buildFailed", res.BuildFailed, "timedOut", res.TimedOut)
	writeJSON(w, http.StatusOK, res)
}

// handleVulncheck проверяет модуль примера govulncheck по снимку базы
// уязвимостей из образа. Код не исполняется, флаги урока не нужны.
func (s *Server) handleVulncheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lesson string        `json:"lesson"`
		Files  sandbox.Files `json:"files"`
	}
	if !decode(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.queueTimeout)
	defer cancel()
	res, err := s.exec.Vulncheck(ctx, req.Files)
	if errors.Is(err, sandbox.ErrNoVulncheck) {
		writeError(w, http.StatusNotImplemented, "в этом runner нет govulncheck")
		return
	}
	if s.fail(w, r, err) {
		return
	}
	s.log.Info("vulncheck", "lesson", req.Lesson, "ms", res.DurationMs, "exit", res.ExitCode, "found", res.Found)
	writeJSON(w, http.StatusOK, res)
}

type testRequest struct {
	Lesson string        `json:"lesson"`
	Files  sandbox.Files `json:"files"`
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if !decode(w, r, &req) {
		return
	}
	l, ok := s.lessons[req.Lesson]
	if !ok || len(l.Tests) == 0 {
		writeError(w, http.StatusNotFound, "у урока %q нет задачи", req.Lesson)
		return
	}
	files := sandbox.Files{}
	for name, src := range req.Files {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			writeError(w, http.StatusBadRequest, "%s: тесты и go.mod задаёт урок, присылайте только .go-файлы решения", name)
			return
		}
		files[name] = src
	}
	for name, src := range l.Tests {
		files[name] = src
	}

	key := hashKey("test", req.Lesson, files, l.Flags)
	if res, ok := s.cache.get(key); ok {
		writeJSON(w, http.StatusOK, res)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.queueTimeout)
	defer cancel()
	res, err := s.exec.Test(ctx, files, l.Flags)
	if s.fail(w, r, err) {
		return
	}
	for i := range res.Tests {
		res.Tests[i].Hidden = l.isHidden(res.Tests[i].Name)
	}
	// Таймаут может быть случайностью нагрузки — такой результат не запоминаем.
	if !res.TimedOut {
		s.cache.put(key, res)
	}
	s.log.Info("test", "lesson", req.Lesson, "ms", res.DurationMs, "passed", res.Passed,
		"buildFailed", res.BuildFailed, "tests", len(res.Tests))
	writeJSON(w, http.StatusOK, res)
}

// handleSolution отдаёт эталон задачи. Учебник для своей команды, поэтому
// условие «после успеха или трёх попыток» проверяет интерфейс, а не сервер.
func (s *Server) handleSolution(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lesson string `json:"lesson"`
	}
	if !decode(w, r, &req) {
		return
	}
	l, ok := s.lessons[req.Lesson]
	if !ok || l.Solution == "" {
		writeError(w, http.StatusNotFound, "у урока %q нет эталона", req.Lesson)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": sandbox.Files{"task.go": l.Solution}})
}

type fmtResponse struct {
	Files  sandbox.Files     `json:"files"`
	Errors map[string]string `json:"errors,omitempty"`
}

// handleFmt форматирует .go-файлы прямо в процессе: go/format не исполняет код.
func (s *Server) handleFmt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Files sandbox.Files `json:"files"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := req.Files.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	resp := fmtResponse{Files: sandbox.Files{}}
	for name, src := range req.Files {
		if !strings.HasSuffix(name, ".go") {
			resp.Files[name] = src
			continue
		}
		out, err := format.Source([]byte(src))
		if err != nil {
			if resp.Errors == nil {
				resp.Errors = map[string]string{}
			}
			resp.Errors[name] = err.Error()
			resp.Files[name] = src
			continue
		}
		resp.Files[name] = string(out)
	}
	writeJSON(w, http.StatusOK, resp)
}

var godebugValue = regexp.MustCompile(`^[a-z0-9_]+=[a-z0-9_.]+(,[a-z0-9_]+=[a-z0-9_.]+)*$`)

func validateFlags(f sandbox.Flags) error {
	if f.GODEBUG != "" && !godebugValue.MatchString(f.GODEBUG) {
		return fmt.Errorf("недопустимое значение GODEBUG %q", f.GODEBUG)
	}
	if f.GOMAXPROCS < 0 || f.GOMAXPROCS > 64 {
		return fmt.Errorf("GOMAXPROCS должен быть от 1 до 64")
	}
	return nil
}

// merge накладывает флаги запроса (живые визуализации) на флаги урока.
func merge(base, over sandbox.Flags) sandbox.Flags {
	if over.Race {
		base.Race = true
	}
	if over.GOMAXPROCS > 0 {
		base.GOMAXPROCS = over.GOMAXPROCS
	}
	if over.GODEBUG != "" {
		base.GODEBUG = over.GODEBUG
	}
	return base
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) bool {
	var inputErr *sandbox.InputError
	switch {
	case err == nil:
		return false
	case errors.As(err, &inputErr):
		writeError(w, http.StatusBadRequest, "%v", err)
	case errors.Is(err, sandbox.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, "все песочницы заняты, попробуйте ещё раз")
	default:
		s.log.Error("sandbox", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "внутренняя ошибка runner")
	}
	return true
}

const maxBody = 1 << 20

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "неверный JSON: %v", err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, args...)})
}

func hashKey(kind, lesson string, files sandbox.Files, flags sandbox.Flags) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%+v\x00", kind, lesson, flags)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%d\x00%s", name, len(files[name]), files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// lru — кеш результатов /test по хешу файлов.
type lru struct {
	mu    sync.Mutex
	cap   int
	order *list.List // фронт — самый свежий
	items map[string]*list.Element
}

type lruEntry struct {
	key string
	val *sandbox.TestResult
}

func newLRU(capacity int) *lru {
	return &lru{cap: capacity, order: list.New(), items: map[string]*list.Element{}}
}

func (c *lru) get(key string) (*sandbox.TestResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry).val, true
}

func (c *lru) put(key string, val *sandbox.TestResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).val = val
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&lruEntry{key, val})
	if c.order.Len() > c.cap {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*lruEntry).key)
	}
}

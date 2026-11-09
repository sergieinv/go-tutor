// Package sandbox собирает и запускает недоверенный Go-код с лимитами.
//
// Сборка идёт от имени самого runner (CGO выключен, сеть модулей закрыта),
// а запуск — через лаунчер, который выставляет rlimit'ы и (под root) меняет
// uid на uid слота. После запуска все процессы слота убиваются, так что
// fork-бомба и брошенные горутины-процессы не переживают запрос.
package sandbox

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Config struct {
	// WorkRoot — каталог для временных модулей; пусто — os.TempDir().
	WorkRoot string
	// UIDs — uid'ы слотов. Их число задаёт параллелизм. Пустой список —
	// запуск без смены uid (локальная разработка, lessonlint) с Slots слотами.
	UIDs  []int
	Slots int
	// Launcher — путь к бинарнику с подкомандой "launch" (обычно сам runner).
	// Пусто — программа запускается напрямую, без rlimit'ов.
	Launcher string

	BuildTimeout time.Duration // лимит на go build
	WallTime     time.Duration // лимит реального времени запуска
	CPUTime      time.Duration // RLIMIT_CPU
	MemoryBytes  int64         // RLIMIT_AS и GOMEMLIMIT
	MaxProcs     int           // RLIMIT_NPROC, только вместе с UIDs
	MaxOutput    int           // лимит stdout и stderr по отдельности
	// BuildEnv добавляется к окружению go build/go vet (GOPROXY, GOCACHE…).
	BuildEnv []string
	// Govulncheck — путь к govulncheck; пусто — /api/vulncheck недоступен.
	Govulncheck string
	// VulnDB — база уязвимостей для govulncheck -db (file:///… — снимок
	// в образе); пусто — база по умолчанию, https://vuln.go.dev.
	VulnDB string
	// Labs — DSN администраторов баз профиля labs: LabPostgres, LabMySQL.
	Labs map[string]string
	// Buf — путь к buf; плагины protoc-gen-* лежат рядом с ним. Пусто —
	// примеры с .proto не собираются, /api/buf недоступен.
	Buf string
	// Pprof — путь к собранному cmd/pprof (в тулчейне с Go 1.25 его нет
	// готовым). Пусто — /api/pprof недоступен.
	Pprof string
}

// DefaultConfig — лимиты из плана: 10 с CPU, 256 МБ, вывод до 64 КБ.
func DefaultConfig() Config {
	return Config{
		Slots:        4,
		BuildTimeout: 60 * time.Second,
		WallTime:     10 * time.Second,
		CPUTime:      10 * time.Second,
		MemoryBytes:  256 << 20,
		MaxProcs:     64,
		MaxOutput:    64 << 10,
	}
}

// Flags — флаги запуска урока из frontmatter.
type Flags struct {
	Race       bool   `json:"race,omitempty" yaml:"race"`
	GOMAXPROCS int    `json:"gomaxprocs,omitempty" yaml:"gomaxprocs"`
	GODEBUG    string `json:"godebug,omitempty" yaml:"godebug"`
	// GCFlags — флаги компилятора для пакета программы (ValidGCFlags): -m —
	// escape analysis и inlining, -d=ssa/check_bce/debug=1 — оставшиеся
	// проверки границ. Диагностика приходит в RunResult.CompilerOutput.
	GCFlags string `json:"gcflags,omitempty" yaml:"gcflags"`
	// GOTRACEBACK — сколько стеков печатать при панике: none, single, all, system.
	GOTRACEBACK string `json:"gotraceback,omitempty" yaml:"gotraceback"`
	// Labs — базы профиля labs, которые нужны программе (LabNames).
	Labs []string `json:"labs,omitempty" yaml:"labs"`
}

// Files — имя файла → содержимое.
type Files map[string]string

type RunResult struct {
	BuildFailed bool   `json:"buildFailed"`
	BuildOutput string `json:"buildOutput,omitempty"`
	// CompilerOutput — диагностика компилятора при успешной сборке с GCFlags.
	CompilerOutput string `json:"compilerOutput,omitempty"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	ExitCode    int    `json:"exitCode"`
	TimedOut    bool   `json:"timedOut"`
	Truncated   bool   `json:"truncated"`
	DurationMs  int64  `json:"durationMs"`
}

type Executor struct {
	cfg   Config
	slots chan int // свободные uid'ы; -1 — без смены uid
	labs  *labs
}

func New(cfg Config) (*Executor, error) {
	e := &Executor{cfg: cfg, labs: newLabs(cfg.Labs)}
	if len(cfg.UIDs) > 0 {
		e.slots = make(chan int, len(cfg.UIDs))
		for _, uid := range cfg.UIDs {
			e.slots <- uid
		}
	} else {
		n := max(cfg.Slots, 1)
		e.slots = make(chan int, n)
		for range n {
			e.slots <- -1
		}
	}
	if _, err := exec.LookPath("go"); err != nil {
		return nil, fmt.Errorf("нужен тулчейн Go: %w", err)
	}
	return e, nil
}

// ErrBusy — все слоты заняты дольше, чем готов ждать вызывающий.
var ErrBusy = errors.New("sandbox: все слоты заняты")

func (e *Executor) acquire(ctx context.Context) (int, error) {
	select {
	case uid := <-e.slots:
		return uid, nil
	case <-ctx.Done():
		return 0, ErrBusy
	}
}

func (e *Executor) release(uid int) { e.slots <- uid }

// go.mod и go.sum по умолчанию: версия языка курса и разрешённые сторонние
// модули (как обновить — в комментарии в deps.mod). Образ runner держит их
// в кеше модулей.
//
//go:embed deps.mod
var depsGoMod string

//go:embed deps.sum
var depsGoSum string

var fileName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*\.(go|mod|sum|proto|txt|json|sql|yaml|lock)$`)

const (
	maxFiles     = 32
	maxTotalSize = 512 << 10
)

// Validate проверяет набор файлов от клиента.
func (f Files) Validate() error {
	if len(f) == 0 {
		return errors.New("нет файлов")
	}
	if len(f) > maxFiles {
		return fmt.Errorf("слишком много файлов: %d, максимум %d", len(f), maxFiles)
	}
	total := 0
	for name, src := range f {
		if !fileName.MatchString(name) {
			return fmt.Errorf("недопустимое имя файла %q", name)
		}
		total += len(src)
		if strings.HasSuffix(name, ".go") && cgoImport.MatchString(src) {
			return fmt.Errorf("%s: cgo (import \"C\") в песочнице не поддерживается", name)
		}
	}
	if total > maxTotalSize {
		return fmt.Errorf("файлы занимают %d байт, максимум %d", total, maxTotalSize)
	}
	return nil
}

var cgoImport = regexp.MustCompile(`(?m)^\s*import\s+(\(\s*)?"C"`)

// workspace — временный модуль запроса. Каталог принадлежит uid слота,
// чтобы программа могла писать в рабочий каталог (уроки про файлы).
type workspace struct {
	dir string
	uid int
	// fileLimit — RLIMIT_FSIZE программы; 0 — defaultFileLimit.
	fileLimit int64
}

// defaultFileLimit — предел размера файла, который может записать программа.
const defaultFileLimit = 16 << 20

// newWorkspace создаёт рабочий каталог и, если в файлах есть .proto,
// генерирует по ним Go-код (*GenerateError — ошибка сборки для ученика).
func (e *Executor) newWorkspace(ctx context.Context, uid int, files Files) (*workspace, error) {
	if err := files.Validate(); err != nil {
		return nil, &InputError{err}
	}
	dir, err := os.MkdirTemp(e.cfg.WorkRoot, "run-*")
	if err != nil {
		return nil, err
	}
	ws := &workspace{dir: dir, uid: uid}
	if _, ok := files["go.mod"]; !ok {
		files = clone(files)
		files["go.mod"] = depsGoMod
		if _, ok := files["go.sum"]; !ok {
			files["go.sum"] = depsGoSum
		}
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			ws.remove()
			return nil, err
		}
	}
	if uid >= 0 {
		if err := os.Chown(dir, uid, uid); err != nil {
			ws.remove()
			return nil, err
		}
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		ws.remove()
		return nil, err
	}
	if err := e.generate(ctx, ws, files); err != nil {
		ws.remove()
		return nil, err
	}
	return ws, nil
}

func (ws *workspace) remove() { os.RemoveAll(ws.dir) }

// InputError — ошибка во входных данных клиента (HTTP 400).
type InputError struct{ Err error }

func (e *InputError) Error() string { return e.Err.Error() }
func (e *InputError) Unwrap() error { return e.Err }

func clone(f Files) Files {
	c := make(Files, len(f)+1)
	for k, v := range f {
		c[k] = v
	}
	return c
}

// goTool запускает команду go (build, vet, test -c) от имени runner.
func (e *Executor) goTool(ctx context.Context, dir string, flags Flags, args ...string) (output string, err error) {
	return e.tool(ctx, dir, flags, "go", args...)
}

// tool запускает инструмент, который читает код, но не исполняет его
// (go build, govulncheck), от имени runner с окружением сборки.
func (e *Executor) tool(ctx context.Context, dir string, flags Flags, name string, args ...string) (output string, err error) {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.BuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cgo := "0"
	if flags.Race {
		cgo = "1" // детектору гонок нужен cgo на части платформ
	}
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod", "GOWORK=off", "CGO_ENABLED="+cgo)
	cmd.Env = append(cmd.Env, e.cfg.BuildEnv...)
	var buf limitedBuffer
	buf.limit = e.cfg.MaxOutput
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err = cmd.Run()
	if ctx.Err() != nil {
		return buf.String() + "\nсборка не уложилась в " + e.cfg.BuildTimeout.String(), ctx.Err()
	}
	return strings.ReplaceAll(buf.String(), dir+string(filepath.Separator), ""), err
}

// Run собирает и запускает программу.
func (e *Executor) Run(ctx context.Context, files Files, flags Flags) (*RunResult, error) {
	res, _, err := e.RunCollect(ctx, files, flags, "")
	return res, err
}

// maxCollect — предел файла, который RunCollect забирает из рабочего каталога.
// RLIMIT_FSIZE программы — defaultFileLimit, так что больше там не бывает.
const maxCollect = defaultFileLimit

// RunCollect как Run, но после запуска читает файл collect из рабочего
// каталога программы (например, трассу runtime/trace). Нет файла — nil.
func (e *Executor) RunCollect(ctx context.Context, files Files, flags Flags, collect string) (*RunResult, []byte, error) {
	var data []byte
	res, err := e.runWith(ctx, files, flags, func(ws *workspace, _ *RunResult) error {
		if collect == "" {
			return nil
		}
		var err error
		data, err = readCapped(filepath.Join(ws.dir, collect), maxCollect)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
	return res, data, err
}

// runWith собирает и запускает программу, а после запуска, пока рабочий
// каталог ещё на месте, вызывает after (если сборка удалась).
func (e *Executor) runWith(ctx context.Context, files Files, flags Flags, after func(ws *workspace, res *RunResult) error) (*RunResult, error) {
	uid, err := e.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer e.release(uid)
	ws, err := e.newWorkspace(ctx, uid, files)
	var genErr *GenerateError
	if errors.As(err, &genErr) {
		return &RunResult{BuildFailed: true, BuildOutput: genErr.Output}, nil
	}
	if err != nil {
		return nil, err
	}
	defer ws.remove()

	args := []string{"build", "-o", "prog"}
	if flags.Race {
		args = append(args, "-race")
	}
	if flags.GCFlags != "" {
		// Без all=: флаги действуют только на пакет программы, не на fmt и рантайм.
		args = append(args, "-gcflags="+flags.GCFlags)
	}
	out, err := e.goTool(ctx, ws.dir, flags, append(args, ".")...)
	if err != nil {
		return &RunResult{BuildFailed: true, BuildOutput: out}, nil
	}
	p := e.exec(ctx, ws, flags, "./prog")
	res := &RunResult{
		Stdout:     p.stdout,
		Stderr:     p.stderr,
		ExitCode:   p.exitCode,
		TimedOut:   p.timedOut,
		Truncated:  p.truncated,
		DurationMs: p.duration.Milliseconds(),
	}
	if flags.GCFlags != "" {
		res.CompilerOutput = compilerOutput(out)
	}
	if after != nil {
		if err := after(ws, res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// compilerOutput убирает из вывода go build строку «# пакет» перед диагностикой.
func compilerOutput(out string) string {
	var b strings.Builder
	for line := range strings.Lines(out) {
		if !strings.HasPrefix(line, "# ") {
			b.WriteString(line)
		}
	}
	return b.String()
}

// gcFlagsAllowed — флаги компилятора, которые урок может включить: только
// диагностика и отключение оптимизаций, ничего, что читает или пишет файлы.
var gcFlagsAllowed = []string{"-m", "-m=1", "-m=2", "-l", "-N", "-d=ssa/check_bce/debug=1"}

// ValidGCFlags проверяет флаги компилятора: слова через пробел из gcFlagsAllowed.
func ValidGCFlags(s string) error {
	for _, f := range strings.Fields(s) {
		if !slices.Contains(gcFlagsAllowed, f) {
			return fmt.Errorf("флаг компилятора %q не поддерживается, допустимы: %s", f, strings.Join(gcFlagsAllowed, " "))
		}
	}
	return nil
}

var tracebackLevels = []string{"none", "single", "all", "system"}

// ValidTraceback проверяет значение GOTRACEBACK.
func ValidTraceback(s string) error {
	if s != "" && !slices.Contains(tracebackLevels, s) {
		return fmt.Errorf("GOTRACEBACK %q не поддерживается, допустимы: %s", s, strings.Join(tracebackLevels, ", "))
	}
	return nil
}

// readCapped читает обычный файл не длиннее limit: программа могла
// подменить его симлинком или каталогом.
func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: не обычный файл", filepath.Base(path))
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

// Build только собирает программу: для вопросов «скомпилируется ли».
func (e *Executor) Build(ctx context.Context, files Files, flags Flags) (ok bool, output string, err error) {
	uid, err := e.acquire(ctx)
	if err != nil {
		return false, "", err
	}
	defer e.release(uid)
	ws, err := e.newWorkspace(ctx, uid, files)
	var genErr *GenerateError
	if errors.As(err, &genErr) {
		return false, genErr.Output, nil
	}
	if err != nil {
		return false, "", err
	}
	defer ws.remove()
	out, err := e.goTool(ctx, ws.dir, flags, "build", "-o", os.DevNull, ".")
	return err == nil, out, nil
}

type procResult struct {
	stdout, stderr string
	exitCode       int
	timedOut       bool
	truncated      bool
	duration       time.Duration
}

// exec запускает собранный бинарник в рабочем каталоге с лимитами.
func (e *Executor) exec(ctx context.Context, ws *workspace, flags Flags, prog string, args ...string) procResult {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.WallTime)
	defer cancel()

	var cmd *exec.Cmd
	if e.cfg.Launcher != "" {
		largs := []string{"launch",
			"-cpu", strconv.Itoa(int(e.cfg.CPUTime.Seconds())),
			"-fsize", strconv.FormatInt(cmp.Or(ws.fileLimit, defaultFileLimit), 10),
			"-nofile", "256",
		}
		// Адресное пространство с запасом: рантайм Go резервирует больше, чем
		// использует. Детектор гонок резервирует терабайты под теневую память,
		// поэтому с -race память держат GOMEMLIMIT и лимит контейнера.
		if !flags.Race {
			largs = append(largs, "-as", strconv.FormatInt(e.cfg.MemoryBytes*4, 10))
		}
		if ws.uid >= 0 {
			largs = append(largs, "-nproc", strconv.Itoa(e.cfg.MaxProcs))
		}
		largs = append(append(largs, "--", prog), args...)
		cmd = exec.Command(e.cfg.Launcher, largs...)
	} else {
		cmd = exec.Command(prog, args...)
	}
	cmd.Dir = ws.dir
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + ws.dir,
		"TMPDIR=" + ws.dir,
		"GOMEMLIMIT=" + strconv.FormatInt(e.cfg.MemoryBytes, 10),
	}
	if flags.GOMAXPROCS > 0 {
		cmd.Env = append(cmd.Env, "GOMAXPROCS="+strconv.Itoa(flags.GOMAXPROCS))
	}
	if flags.GODEBUG != "" {
		cmd.Env = append(cmd.Env, "GODEBUG="+flags.GODEBUG)
	}
	if flags.GOTRACEBACK != "" {
		cmd.Env = append(cmd.Env, "GOTRACEBACK="+flags.GOTRACEBACK)
	}
	labEnv, labNote, dropLabs := e.labs.prepare(ctx, flags.Labs)
	defer dropLabs()
	cmd.Env = append(cmd.Env, labEnv...)
	cmd.SysProcAttr = sysProcAttr(ws.uid)

	// Превышение лимита вывода обрывает программу: бесконечная печать
	// иначе занимала бы слот до таймаута.
	overflow := newSignal()
	stdout := &limitedBuffer{limit: e.cfg.MaxOutput, onOverflow: overflow}
	stderr := &limitedBuffer{limit: e.cfg.MaxOutput, onOverflow: overflow}
	cmd.Stdout, cmd.Stderr = stdout, stderr

	start := time.Now()
	res := procResult{exitCode: -1}
	if err := cmd.Start(); err != nil {
		res.stderr = err.Error()
		return res
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		res.timedOut = true
		killGroup(cmd)
		err = <-done
	case <-overflow.C:
		killGroup(cmd)
		err = <-done
	}
	res.duration = time.Since(start)
	e.killSlot(ws.uid)

	res.stdout, res.stderr = stdout.String(), labNote+stderr.String()
	res.truncated = stdout.overflowed || stderr.overflowed
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.exitCode = 0
	case errors.As(err, &exitErr):
		res.exitCode = exitErr.ExitCode()
		// Кончился RLIMIT_CPU: либо сигнал напрямую, либо Go-рантайм
		// перехватил SIGXCPU, напечатал стек и вышел с кодом 2.
		st, ok := exitErr.Sys().(syscall.WaitStatus)
		if ok && st.Signaled() && st.Signal() == syscall.SIGXCPU ||
			res.exitCode == 2 && strings.HasPrefix(res.stderr, "SIGXCPU: ") {
			res.timedOut = true
		}
	}
	return res
}

// killSlot убивает все процессы uid слота — то, что сбежало из группы.
func (e *Executor) killSlot(uid int) {
	if uid < 0 || e.cfg.Launcher == "" {
		return
	}
	cmd := exec.Command(e.cfg.Launcher, "killall")
	cmd.SysProcAttr = sysProcAttr(uid)
	_ = cmd.Run()
}

// limitedBuffer хранит не больше limit байт и сигналит о переполнении.
type limitedBuffer struct {
	buf        bytes.Buffer
	limit      int
	overflowed bool
	onOverflow *signal
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.buf.Write(p[:max(room, 0)])
		if !b.overflowed {
			b.overflowed = true
			if b.onOverflow != nil {
				b.onOverflow.fire()
			}
		}
		// Сообщаем об успехе, чтобы писатель не получил EPIPE раньше kill.
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string { return b.buf.String() }

// signal — канал, который можно безопасно закрыть из нескольких горутин.
type signal struct {
	C    chan struct{}
	once sync.Once
}

func newSignal() *signal { return &signal{C: make(chan struct{})} }

func (s *signal) fire() { s.once.Do(func() { close(s.C) }) }

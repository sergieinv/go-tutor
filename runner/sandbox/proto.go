package sandbox

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Конфигурация buf по умолчанию для примеров с .proto: зависимости
// (googleapis, protovalidate) лежат в кеше buf образа по коммитам из
// buf.lock, плагины — protoc-gen-go, protoc-gen-go-grpc, protoc-gen-grpc-gateway.
//
//go:embed proto/buf.yaml proto/buf.lock proto/buf.gen.yaml
var protoDefaults embed.FS

// Сгенерированные файлы: перед генерацией runner их удаляет, чтобы
// переименованный или удалённый .proto не оставил устаревший код.
var generatedSuffixes = []string{".pb.go", "_grpc.pb.go", ".pb.gw.go"}

// maxProtoBytes — предел суммарного размера .proto. buf и плагины работают
// от имени runner вне лимитов слота, а контракт на сотни килобайт с
// рекурсивными сообщениями даёт десятки мегабайт кода и сотни мегабайт памяти.
// Контракты уроков — единицы килобайт.
const maxProtoBytes = 64 << 10

func checkProtoSize(files Files) error {
	n := 0
	for name, src := range files {
		if strings.HasSuffix(name, ".proto") {
			n += len(src)
		}
	}
	if n > maxProtoBytes {
		return fmt.Errorf(".proto вместе больше %d КиБ", maxProtoBytes>>10)
	}
	return nil
}

func hasProto(files Files) bool {
	for name := range files {
		if strings.HasSuffix(name, ".proto") {
			return true
		}
	}
	return false
}

// GenerateError — buf generate не смог собрать код из .proto: для ученика
// это ошибка сборки.
type GenerateError struct{ Output string }

func (e *GenerateError) Error() string { return "buf generate: " + e.Output }

// writeBufConfig кладёт в каталог конфигурацию buf. buf.gen.yaml и buf.lock
// всегда свои: плагины генерации — это программы, которые buf запускает от
// имени runner, а buf.lock держит зависимости в пределах кеша образа.
// buf.yaml клиента принимается, только если в нём настройки lint и breaking.
func writeBufConfig(dir string, files Files) error {
	for _, name := range []string{"buf.gen.yaml", "buf.lock", "buf.yaml"} {
		data, err := protoDefaults.ReadFile("proto/" + name)
		if err != nil {
			return err
		}
		if name == "buf.yaml" {
			if own, ok := files["buf.yaml"]; ok {
				canon, err := checkBufYAML(own, string(data))
				if err != nil {
					return &InputError{err}
				}
				data = canon
			}
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// bufYAML — то, что разрешено в buf.yaml примера.
type bufYAML struct {
	Version  string    `yaml:"version"`
	Deps     []string  `yaml:"deps,omitempty"`
	Lint     *bufRules `yaml:"lint,omitempty"`
	Breaking *bufRules `yaml:"breaking,omitempty"`
}

type bufRules struct {
	Use    []string `yaml:"use,omitempty"`
	Except []string `yaml:"except,omitempty"`
}

// checkBufYAML отвергает buf.yaml с чем-то, кроме правил lint и breaking
// (например, plugins в buf.yaml запускают локальные программы проверок),
// и возвращает его каноническую форму — на диск попадает только то, что
// разобрано здесь, а не исходный текст, который buf мог бы прочитать иначе.
func checkBufYAML(src, defaults string) ([]byte, error) {
	var own, def bufYAML
	const allowed = "buf.yaml: в песочнице допустимы только version, deps, lint.use/except и breaking.use/except"
	dec := yaml.NewDecoder(strings.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&own); err != nil {
		return nil, fmt.Errorf("%s: %v", allowed, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%s, и только один документ YAML", allowed)
	}
	if err := yaml.Unmarshal([]byte(defaults), &def); err != nil {
		return nil, err
	}
	if own.Version != "v2" {
		return nil, errors.New("buf.yaml: нужна version: v2")
	}
	for _, d := range own.Deps {
		if !slices.Contains(def.Deps, d) {
			return nil, fmt.Errorf("buf.yaml: зависимость %s недоступна без сети, есть только %s", d, strings.Join(def.Deps, ", "))
		}
	}
	return yaml.Marshal(own)
}

// generate удаляет старый сгенерированный код и запускает buf generate.
func (e *Executor) generate(ctx context.Context, ws *workspace, files Files) error {
	if !hasProto(files) {
		return nil
	}
	if e.cfg.Buf == "" {
		return &GenerateError{"в этом runner нет buf: примеры с .proto не собираются"}
	}
	if err := checkProtoSize(files); err != nil {
		return &GenerateError{err.Error()}
	}
	entries, err := os.ReadDir(ws.dir)
	if err != nil {
		return err
	}
	for _, en := range entries {
		if IsGenerated(en.Name()) {
			os.Remove(filepath.Join(ws.dir, en.Name()))
		}
	}
	if err := writeBufConfig(ws.dir, files); err != nil {
		return err
	}
	out, err := e.buf(ctx, ws.dir, "generate")
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exitErr) || errors.Is(err, context.DeadlineExceeded):
		return &GenerateError{out}
	default:
		return err
	}
}

// GenerateFiles генерирует Go-код по .proto из files и возвращает только
// сгенерированные файлы — lessonlint сверяет с ними закоммиченный код.
func (e *Executor) GenerateFiles(ctx context.Context, files Files) (Files, error) {
	if err := files.Validate(); err != nil {
		return nil, &InputError{err}
	}
	dir, err := os.MkdirTemp(e.cfg.WorkRoot, "gen-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ws := &workspace{dir: dir, uid: -1}
	for name, src := range protoOnly(files) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			return nil, err
		}
	}
	if err := e.generate(ctx, ws, files); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := Files{}
	for _, en := range entries {
		if IsGenerated(en.Name()) {
			b, err := os.ReadFile(filepath.Join(dir, en.Name()))
			if err != nil {
				return nil, err
			}
			out[en.Name()] = string(b)
		}
	}
	return out, nil
}

// IsGenerated сообщает, что файл — код, сгенерированный по .proto.
func IsGenerated(name string) bool {
	for _, suf := range generatedSuffixes {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

// buf запускает buf от имени runner. Плагины ищутся рядом с бинарником buf,
// кеш зависимостей — в BUF_CACHE_DIR окружения runner.
func (e *Executor) buf(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.BuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.cfg.Buf, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(e.cfg.Buf)+string(os.PathListSeparator)+os.Getenv("PATH"))
	// По таймауту убиваем всю группу: плагины — дочерние процессы buf.
	cmd.SysProcAttr = sysProcAttr(-1)
	cmd.Cancel = func() error { killGroup(cmd); return nil }
	cmd.WaitDelay = time.Second
	var out limitedBuffer
	out.limit = e.cfg.MaxOutput
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.ReplaceAll(out.String(), dir+string(filepath.Separator), "")
	if ctx.Err() != nil {
		return text + "\nbuf не уложился в " + e.cfg.BuildTimeout.String(), ctx.Err()
	}
	return text, err
}

// BufResult — вывод buf lint или buf breaking.
type BufResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
	// Found — buf нашёл нарушения (код выхода 100).
	Found bool `json:"found"`
	// Failed — buf не смог проверить: .proto не компилируется и т.п.
	Failed     bool  `json:"failed"`
	TimedOut   bool  `json:"timedOut"`
	DurationMs int64 `json:"durationMs"`
}

// ErrNoBuf — runner собран без buf.
var ErrNoBuf = errors.New("sandbox: buf не настроен")

// BufCheck запускает buf lint (against == nil) или buf breaking против
// прежней версии контракта against.
func (e *Executor) BufCheck(ctx context.Context, files, against Files) (*BufResult, error) {
	if e.cfg.Buf == "" {
		return nil, ErrNoBuf
	}
	if !hasProto(files) || against != nil && !hasProto(against) {
		return nil, &InputError{errors.New("нет файлов .proto")}
	}
	for _, f := range []Files{files, against} {
		if f == nil {
			continue
		}
		if err := f.Validate(); err != nil {
			return nil, &InputError{err}
		}
		if err := checkProtoSize(f); err != nil {
			return nil, &InputError{err}
		}
	}
	uid, err := e.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer e.release(uid)
	// Модули buf — в подкаталогах current/ и against/, корень пуст.
	ws, err := e.newWorkspace(ctx, uid, Files{"README.txt": ""})
	if err != nil {
		return nil, err
	}
	defer ws.remove()

	cur := filepath.Join(ws.dir, "current")
	args := []string{"lint", "current", "--error-format=json"}
	if against != nil {
		prev := filepath.Join(ws.dir, "against")
		if err := writeModule(prev, protoOnly(against)); err != nil {
			return nil, err
		}
		args = []string{"breaking", "current", "--against", "against", "--error-format=json"}
	}
	if err := writeModule(cur, protoOnly(files)); err != nil {
		return nil, err
	}
	start := time.Now()
	out, err := e.buf(ctx, ws.dir, args...)
	res := &BufResult{Output: out, DurationMs: time.Since(start).Milliseconds()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		res.ExitCode, res.Failed, res.TimedOut = -1, true, true
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 100:
		res.ExitCode, res.Found = 100, true
		res.Output = formatViolations(out)
	case errors.As(err, &exitErr):
		res.ExitCode, res.Failed = exitErr.ExitCode(), true
	default:
		return nil, err
	}
	return res, nil
}

// formatViolations превращает JSON-строки buf в «файл:строка:столбец ПРАВИЛО сообщение»:
// в тексте по умолчанию buf не пишет имя правила, а по нему ищут документацию.
func formatViolations(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var v struct {
			Path    string `json:"path"`
			Line    int    `json:"start_line"`
			Column  int    `json:"start_column"`
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(line), &v) != nil || v.Type == "" {
			b.WriteString(line + "\n")
			continue
		}
		path, _ := strings.CutPrefix(v.Path, "current/")
		fmt.Fprintf(&b, "%s:%d:%d %s %s\n", path, v.Line, v.Column, v.Type, v.Message)
	}
	return b.String()
}

// protoOnly — .proto и buf.yaml: остальное buf lint/breaking не нужно.
func protoOnly(files Files) Files {
	out := Files{}
	for name, src := range files {
		if strings.HasSuffix(name, ".proto") || name == "buf.yaml" {
			out[name] = src
		}
	}
	return out
}

// writeModule создаёт каталог модуля buf с файлами и конфигурацией.
func writeModule(dir string, files Files) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			return err
		}
	}
	return writeBufConfig(dir, files)
}

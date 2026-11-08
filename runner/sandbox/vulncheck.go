package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// VulncheckResult — вывод govulncheck по модулю ученика.
type VulncheckResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
	// Found — код вызывает уязвимые функции (govulncheck выходит с кодом 3).
	Found bool `json:"found"`
	// Failed — govulncheck не смог проверить код: не собирается, нет модуля в кеше.
	Failed   bool `json:"failed"`
	TimedOut bool `json:"timedOut"`
	// DBModified — дата снимка базы уязвимостей (ГГГГ-ММ-ДД), если он локальный.
	DBModified string `json:"dbModified,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

// ErrNoVulncheck — runner собран без govulncheck.
var ErrNoVulncheck = errors.New("sandbox: govulncheck не настроен")

// Vulncheck запускает govulncheck в режиме исходников. Программа ученика
// не исполняется: govulncheck только загружает пакеты через go list и строит
// граф вызовов, поэтому он работает от имени runner, как go build.
func (e *Executor) Vulncheck(ctx context.Context, files Files) (*VulncheckResult, error) {
	if e.cfg.Govulncheck == "" {
		return nil, ErrNoVulncheck
	}
	uid, err := e.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer e.release(uid)
	ws, err := e.newWorkspace(uid, files)
	if err != nil {
		return nil, err
	}
	defer ws.remove()

	var args []string
	if e.cfg.VulnDB != "" {
		args = append(args, "-db", e.cfg.VulnDB)
	}
	start := time.Now()
	out, err := e.tool(ctx, ws.dir, Flags{}, e.cfg.Govulncheck, append(args, "./...")...)
	res := &VulncheckResult{Output: out, DBModified: e.vulnDBModified(), DurationMs: time.Since(start).Milliseconds()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 3:
		res.ExitCode, res.Found = 3, true
	case errors.As(err, &exitErr):
		res.ExitCode, res.Failed = exitErr.ExitCode(), true
	case errors.Is(err, context.DeadlineExceeded):
		res.ExitCode, res.Failed, res.TimedOut = -1, true, true
	default:
		return nil, err
	}
	return res, nil
}

// vulnDBModified читает дату снимка из index/db.json локальной базы.
func (e *Executor) vulnDBModified() string {
	u, err := url.Parse(e.cfg.VulnDB)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(u.Path, "index", "db.json"))
	if err != nil {
		return ""
	}
	var db struct {
		Modified time.Time `json:"modified"`
	}
	if json.Unmarshal(b, &db) != nil || db.Modified.IsZero() {
		return ""
	}
	return db.Modified.UTC().Format(time.DateOnly)
}

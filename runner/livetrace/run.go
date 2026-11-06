package livetrace

import (
	"bytes"
	"context"
	"errors"

	"gotutor/runner/sandbox"
)

// Result — ответ /api/trace: обычный результат запуска плюс разобранная трасса.
type Result struct {
	*sandbox.RunResult
	Trace *Trace `json:"trace,omitempty"`
	// TraceError — почему трассы нет, по-русски для ученика.
	TraceError string `json:"traceError,omitempty"`
}

// Run размечает код, запускает его с трассировкой и разбирает трассу.
// Детектор гонок выключен: он в разы замедляет программу и искажает тайминги.
func Run(ctx context.Context, exec *sandbox.Executor, files sandbox.Files, flags sandbox.Flags) (*Result, error) {
	if err := files.Validate(); err != nil {
		return nil, &sandbox.InputError{Err: err}
	}
	marked, selects, err := Instrument(files)
	if err != nil {
		return nil, &sandbox.InputError{Err: err}
	}
	flags.Race = false
	run, data, err := exec.RunCollect(ctx, marked, flags, TraceFile)
	if err != nil {
		return nil, err
	}
	res := &Result{RunResult: run}
	if run.BuildFailed {
		return res, nil
	}
	if data == nil {
		res.TraceError = "Программа не записала трассу: она завершилась через os.Exit, паникой в горутине, deadlock'ом или по таймауту."
		return res, nil
	}
	t, err := Parse(bytes.NewReader(data), selects)
	switch {
	case errors.Is(err, ErrNoTrace):
		res.TraceError = "Трасса оборвана: программа завершилась раньше, чем main вернул управление (os.Exit, паника в горутине, deadlock или таймаут)."
	case err != nil:
		res.TraceError = err.Error()
	default:
		res.Trace = t
	}
	return res, nil
}

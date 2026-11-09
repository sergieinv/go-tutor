package sandbox

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
)

// ProfileResult — запуск программы и отчёты pprof по профилям, которые
// она записала в рабочий каталог через runtime/pprof.
type ProfileResult struct {
	*RunResult
	Profiles []Profile `json:"profiles"`
}

// Profile — отчёт pprof -top по одному файлу *.pprof.
type Profile struct {
	Name string `json:"name"`
	Top  string `json:"top"`
	// Failed — pprof не смог прочитать файл (он пуст или это не профиль).
	Failed bool `json:"failed,omitempty"`
}

// ErrNoPprof — runner собран без pprof.
var ErrNoPprof = errors.New("sandbox: pprof не настроен")

var profileName = regexp.MustCompile(`^[A-Za-z0-9_-]+\.pprof$`)

const (
	maxProfiles = 4
	topNodes    = "20"
)

// Profile собирает и запускает программу, затем строит pprof -top по каждому
// *.pprof в рабочем каталоге (не больше maxProfiles). Профиль записала
// программа ученика, то есть это недоверенные данные, поэтому pprof работает
// так же, как сама программа: от имени uid слота и с теми же лимитами.
func (e *Executor) Profile(ctx context.Context, files Files, flags Flags) (*ProfileResult, error) {
	if e.cfg.Pprof == "" {
		return nil, ErrNoPprof
	}
	res := &ProfileResult{Profiles: []Profile{}}
	run, err := e.runWith(ctx, files, flags, func(ws *workspace, run *RunResult) error {
		entries, err := os.ReadDir(ws.dir)
		if err != nil {
			return err
		}
		var names []string
		for _, ent := range entries {
			// Type() не следует по симлинкам: профиль-ссылку на чужой файл не читаем.
			if ent.Type().IsRegular() && profileName.MatchString(ent.Name()) {
				names = append(names, ent.Name())
			}
		}
		slices.Sort(names)
		for _, name := range names[:min(len(names), maxProfiles)] {
			p := e.exec(ctx, ws, Flags{}, e.cfg.Pprof, "-top", "-nodecount="+topNodes, "./prog", name)
			out := strings.ReplaceAll(p.stdout+p.stderr, ws.dir+string(os.PathSeparator), "")
			res.Profiles = append(res.Profiles, Profile{Name: name, Top: out, Failed: p.exitCode != 0})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.RunResult = run
	return res, nil
}

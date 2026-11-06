package livetrace

import (
	"bufio"
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gotutor/runner/sandbox"
)

// RaceReport — один отчёт детектора гонок: два конфликтующих доступа
// к одному адресу и где созданы участвовавшие горутины.
type RaceReport struct {
	Accesses   []RaceAccess    `json:"accesses"`
	Goroutines []RaceGoroutine `json:"goroutines"`
}

// RaceAccess — доступ к памяти. Первый в отчёте — текущий, второй — предыдущий.
type RaceAccess struct {
	Kind string `json:"kind"` // read | write
	// G — номер горутины; 0 — main.
	G     int64       `json:"g"`
	Addr  string      `json:"addr"`
	Stack []RaceFrame `json:"stack"`
}

// RaceGoroutine — горутина из отчёта и место её создания.
type RaceGoroutine struct {
	G       int64       `json:"g"`
	State   string      `json:"state"` // running | finished
	Created []RaceFrame `json:"created"`
}

// RaceFrame — кадр стека из кода ученика (пакет main).
type RaceFrame struct {
	Func string `json:"func"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// RaceResult — ответ /api/race.
type RaceResult struct {
	*sandbox.RunResult
	Races []RaceReport `json:"races"`
}

// RunRace запускает программу с -race и разбирает отчёты детектора из stderr.
func RunRace(ctx context.Context, exec *sandbox.Executor, files sandbox.Files, flags sandbox.Flags) (*RaceResult, error) {
	flags.Race = true
	res, err := exec.Run(ctx, files, flags)
	if err != nil {
		return nil, err
	}
	return &RaceResult{RunResult: res, Races: ParseRace(res.Stderr)}, nil
}

var (
	accessLine    = regexp.MustCompile(`^(Previous )?(read|write|Read|Write) at (0x[0-9a-f]+) by (main goroutine|goroutine (\d+)):$`)
	goroutineLine = regexp.MustCompile(`^Goroutine (\d+) \(([a-z]+)\) created at:$`)
	frameFunc     = regexp.MustCompile(`^  (\S+)\(.*\)$`)
	frameFile     = regexp.MustCompile(`^      (\S+):(\d+)(?: \+0x[0-9a-f]+)?$`)
)

// ParseRace разбирает отчёты «WARNING: DATA RACE». В стеках остаются только
// кадры пакета main: рантайм и стандартная библиотека ученику не нужны.
func ParseRace(stderr string) []RaceReport {
	reports := []RaceReport{}
	var cur *RaceReport
	var stack *[]RaceFrame
	var fn string
	sc := bufio.NewScanner(strings.NewReader(stderr))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "WARNING: DATA RACE":
			reports = append(reports, RaceReport{Accesses: []RaceAccess{}, Goroutines: []RaceGoroutine{}})
			cur, stack = &reports[len(reports)-1], nil
		case cur == nil:
		case line == "==================":
			cur, stack = nil, nil
		default:
			if m := accessLine.FindStringSubmatch(line); m != nil {
				a := RaceAccess{Kind: strings.ToLower(m[2]), Addr: m[3], Stack: []RaceFrame{}}
				if m[5] != "" {
					a.G, _ = strconv.ParseInt(m[5], 10, 64)
				}
				cur.Accesses = append(cur.Accesses, a)
				stack = &cur.Accesses[len(cur.Accesses)-1].Stack
			} else if m := goroutineLine.FindStringSubmatch(line); m != nil {
				g, _ := strconv.ParseInt(m[1], 10, 64)
				cur.Goroutines = append(cur.Goroutines, RaceGoroutine{G: g, State: m[2], Created: []RaceFrame{}})
				stack = &cur.Goroutines[len(cur.Goroutines)-1].Created
			} else if m := frameFunc.FindStringSubmatch(line); m != nil {
				fn = m[1]
			} else if m := frameFile.FindStringSubmatch(line); m != nil && stack != nil && strings.HasPrefix(fn, "main.") {
				n, _ := strconv.Atoi(m[2])
				*stack = append(*stack, RaceFrame{Func: fn, File: filepath.Base(m[1]), Line: n})
			}
		}
	}
	return reports
}

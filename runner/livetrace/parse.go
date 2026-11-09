package livetrace

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/exp/trace"
)

// Trace — то, что произошло с горутинами ученика, в порядке времени.
type Trace struct {
	Goroutines []Goroutine `json:"goroutines"`
	Selects    []Select    `json:"selects"`
	Events     []Event     `json:"events"`
	// Total — сколько событий было до обрезки по MaxEvents.
	Total int `json:"total"`
	// DurationUs — от старта main до последнего события.
	DurationUs int64 `json:"durationUs"`
	// Spans — пользовательские задачи и регионы runtime/trace (trace.NewTask,
	// trace.StartRegion / WithRegion), Logs — trace.Log: из них построитель
	// spans рисует запросы, их участки и заметки вроде «осталось 180 мс».
	Spans []Span    `json:"spans"`
	Logs  []UserLog `json:"logs"`
}

// Span — задача (kind task) или регион (kind region) на шкале времени.
type Span struct {
	// ID — у задачи её id в трассе, у региона — порядковый номер региона.
	ID   int64  `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Task — у задачи родительская задача (0 — нет), у региона — задача,
	// в которой он открыт.
	Task int64 `json:"task"`
	G    int64 `json:"g"`
	// Start, End — микросекунды от старта main; End -1 — не закрыт к концу.
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	// Depth — вложенность региона в горутине (0 — внешний).
	Depth int `json:"depth"`
}

// UserLog — запись trace.Log(ctx, category, message).
type UserLog struct {
	T        int64  `json:"t"`
	G        int64  `json:"g"`
	Task     int64  `json:"task"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// MaxSpans — сколько задач, регионов и логов отдаём клиенту (каждого вида).
const MaxSpans = 400

// Goroutine — горутина из кода ученика (main или go-вызов).
type Goroutine struct {
	ID int64 `json:"id"`
	// Func — стартовая функция: main.main, main.worker, main.main.func1 или
	// библиотечная, если горутину запустил wg.Go / errgroup.Go.
	Func string `json:"func"`
	// File:Line — где стоит оператор go; у main пусто.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// Event — событие горутины G.
//
//	go       G создала горутину Target в File:Line
//	block    G заблокировалась в File:Line, Reason: select, chan send, chan receive, sleep…
//	unblock  G разбудила горутину Target; G == 0 — рантайм (таймер, сеть)
//	select   G вошла в select Select
//	case     в select Select выбрана ветка Case
//	end      G завершилась
type Event struct {
	T      int64  `json:"t"` // микросекунды от старта main
	G      int64  `json:"g"`
	Kind   string `json:"kind"`
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Reason string `json:"reason,omitempty"`
	Target int64  `json:"target,omitempty"`
	Select int    `json:"select,omitempty"`
	Case   int    `json:"case"`
}

// MaxEvents — сколько событий отдаём клиенту; счётчики веток считаются по всем.
const MaxEvents = 5000

// ErrNoTrace — программа не записала трассу: os.Exit, паника в горутине,
// deadlock или таймаут обрывают её раньше, чем trace.Stop допишет файл.
var ErrNoTrace = errors.New("трасса не записана")

// Parse разбирает трассу runtime/trace. Трассу пишет недоверенная программа,
// поэтому паника парсера превращается в ошибку.
func Parse(r io.Reader, selects []Select) (t *Trace, err error) {
	defer func() {
		if p := recover(); p != nil {
			t, err = nil, fmt.Errorf("трасса повреждена: %v", p)
		}
	}()
	rd, err := trace.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoTrace, err)
	}
	p := traceParser{
		t:       &Trace{Selects: selects, Events: []Event{}, Goroutines: []Goroutine{}, Spans: []Span{}, Logs: []UserLog{}},
		user:    map[trace.GoID]bool{},
		byID:    map[int]*Select{},
		start:   -1,
		tasks:   map[trace.TaskID]int{},
		regions: map[trace.GoID][]int{},
	}
	for i := range p.t.Selects {
		p.byID[p.t.Selects[i].ID] = &p.t.Selects[i]
	}
	for {
		ev, err := rd.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Хвост трассы бывает недописан — отдаём то, что успели прочитать.
			if p.start < 0 {
				return nil, fmt.Errorf("%w: %v", ErrNoTrace, err)
			}
			break
		}
		p.event(ev)
	}
	if p.start < 0 {
		return nil, ErrNoTrace
	}
	return p.t, nil
}

type traceParser struct {
	t     *Trace
	user  map[trace.GoID]bool
	byID  map[int]*Select
	start trace.Time // время лога gotutor.main; до него — служебный старт трассы
	last  trace.Time
	// tasks — индекс задачи в Spans, regions — стек открытых регионов горутины.
	tasks   map[trace.TaskID]int
	regions map[trace.GoID][]int
	regionN int64
}

func (p *traceParser) event(ev trace.Event) {
	switch ev.Kind() {
	case trace.EventLog:
		p.log(ev)
	case trace.EventStateTransition:
		if p.start >= 0 {
			p.transition(ev)
		}
	case trace.EventTaskBegin, trace.EventTaskEnd, trace.EventRegionBegin, trace.EventRegionEnd:
		if p.start >= 0 {
			p.span(ev)
		}
	}
}

// span отмечает начало и конец пользовательских задач и регионов.
func (p *traceParser) span(ev trace.Event) {
	t := max(ev.Time().Sub(p.start).Microseconds(), 0)
	g := ev.Goroutine()
	switch ev.Kind() {
	case trace.EventTaskBegin:
		task := ev.Task()
		if len(p.t.Spans) >= 2*MaxSpans {
			return
		}
		parent := int64(task.Parent)
		if task.Parent == trace.NoTask {
			parent = 0
		}
		p.tasks[task.ID] = len(p.t.Spans)
		p.t.Spans = append(p.t.Spans, Span{ID: int64(task.ID), Kind: "task", Name: task.Type, Task: parent, G: int64(g), Start: t, End: -1})
	case trace.EventTaskEnd:
		if i, ok := p.tasks[ev.Task().ID]; ok {
			p.t.Spans[i].End = t
		}
	case trace.EventRegionBegin:
		r := ev.Region()
		if len(p.t.Spans) >= 2*MaxSpans {
			return
		}
		stack := p.regions[g]
		p.regions[g] = append(stack, len(p.t.Spans))
		p.regionN++
		p.t.Spans = append(p.t.Spans, Span{ID: p.regionN, Kind: "region", Name: r.Type, Task: int64(r.Task), G: int64(g), Start: t, End: -1, Depth: len(stack)})
	case trace.EventRegionEnd:
		stack := p.regions[g]
		if len(stack) == 0 {
			return
		}
		p.t.Spans[stack[len(stack)-1]].End = t
		p.regions[g] = stack[:len(stack)-1]
	}
	if ev.Time() > p.last {
		p.last = ev.Time()
		p.t.DurationUs = p.last.Sub(p.start).Microseconds()
	}
}

func (p *traceParser) log(ev trace.Event) {
	l := ev.Log()
	g := ev.Goroutine()
	switch l.Category {
	case "gotutor.main":
		if p.start >= 0 {
			return
		}
		p.start = ev.Time()
		p.user[g] = true
		p.t.Goroutines = append(p.t.Goroutines, Goroutine{ID: int64(g), Func: "main.main"})
	case "gotutor.select":
		id, err := strconv.Atoi(l.Message)
		if err != nil || p.byID[id] == nil || !p.user[g] {
			return
		}
		p.add(ev, Event{G: int64(g), Kind: "select", Select: id, File: p.byID[id].File, Line: p.byID[id].Line})
	case "gotutor.case":
		sid, cid, ok := strings.Cut(l.Message, ":")
		id, err1 := strconv.Atoi(sid)
		c, err2 := strconv.Atoi(cid)
		s := p.byID[id]
		if !ok || err1 != nil || err2 != nil || s == nil || c < 0 || c >= len(s.Cases) || !p.user[g] {
			return
		}
		s.Counts[c]++
		p.add(ev, Event{G: int64(g), Kind: "case", Select: id, Case: c, File: s.File, Line: s.Cases[c].Line})
	default:
		if p.start < 0 || len(p.t.Logs) >= MaxSpans || strings.HasPrefix(l.Category, "gotutor.") {
			return
		}
		p.t.Logs = append(p.t.Logs, UserLog{T: max(ev.Time().Sub(p.start).Microseconds(), 0), G: int64(g), Task: int64(l.Task), Category: l.Category, Message: l.Message})
		if ev.Time() > p.last {
			p.last = ev.Time()
			p.t.DurationUs = p.last.Sub(p.start).Microseconds()
		}
	}
}

func (p *traceParser) transition(ev trace.Event) {
	st := ev.StateTransition()
	if st.Resource.Kind != trace.ResourceGoroutine {
		return
	}
	g := st.Resource.Goroutine()
	from, to := st.Goroutine()
	actor := ev.Goroutine()
	switch {
	case from == trace.GoNotExist && to == trace.GoRunnable:
		// Горутина ученика — та, что создана кодом ученика: оператором go
		// в пакете main или через wg.Go / errgroup.Go (тогда стартовая функция
		// из библиотеки). Служебные горутины рантайма (GC и т.п.) пропускаем.
		_, file, line := userFrame(ev.Stack())
		if !p.user[actor] || line == 0 {
			return
		}
		fn, _, _ := userFrame(st.Stack)
		if fn == "" {
			fn = topFunc(st.Stack)
			if fn == "" || strings.HasPrefix(fn, "runtime.") {
				return
			}
		}
		p.user[g] = true
		p.t.Goroutines = append(p.t.Goroutines, Goroutine{ID: int64(g), Func: fn, File: file, Line: line})
		p.add(ev, Event{G: int64(actor), Kind: "go", Target: int64(g), File: file, Line: line})
	case !p.user[g]:
	case from.Executing() && to == trace.GoWaiting:
		_, file, line := userFrame(ev.Stack())
		p.add(ev, Event{G: int64(g), Kind: "block", Reason: st.Reason, File: file, Line: line})
	case from == trace.GoWaiting && to == trace.GoRunnable:
		e := Event{Kind: "unblock", Target: int64(g)}
		if p.user[actor] {
			e.G = int64(actor)
			_, e.File, e.Line = userFrame(ev.Stack())
		}
		p.add(ev, e)
	case to == trace.GoNotExist:
		p.add(ev, Event{G: int64(g), Kind: "end"})
	}
}

func (p *traceParser) add(ev trace.Event, e Event) {
	p.t.Total++
	if ev.Time() > p.last {
		p.last = ev.Time()
		p.t.DurationUs = p.last.Sub(p.start).Microseconds()
	}
	if len(p.t.Events) >= MaxEvents {
		return
	}
	e.T = max(ev.Time().Sub(p.start).Microseconds(), 0)
	p.t.Events = append(p.t.Events, e)
}

// topFunc — функция верхнего кадра стека.
func topFunc(s trace.Stack) string {
	for f := range s.Frames() {
		return f.Func
	}
	return ""
}

// userFrame — верхний кадр стека из пакета main. gotutorMain показываем как main.
func userFrame(s trace.Stack) (fn, file string, line int) {
	for f := range s.Frames() {
		if !strings.HasPrefix(f.Func, "main.") || strings.HasPrefix(f.Func, "main.gotutorSelect") ||
			strings.HasPrefix(f.Func, "main.gotutorCase") || f.Func == "main.main" && filepath.Base(f.File) == generatedFile {
			continue
		}
		return strings.Replace(f.Func, "main.gotutorMain", "main.main", 1), filepath.Base(f.File), int(f.Line)
	}
	return "", "", 0
}

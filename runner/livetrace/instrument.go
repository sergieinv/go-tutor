// Package livetrace запускает программу ученика с runtime/trace и разбирает
// трассу в события для живых визуализаций (POST /api/trace).
//
// Код ученика размечается вставками текста на тех же строках, поэтому номера
// строк в стеках трассы совпадают с тем, что ученик видит в редакторе:
//
//	select {            →  gotutorSelect(1); select {
//	case v := <-a:      →  case v := <-a: gotutorCase(1, 0);
//	func main() {       →  func gotutorMain() {
//
// Сгенерированный файл gotutor_trace.go объявляет настоящий main: он включает
// трассировку, вызывает gotutorMain и останавливает трассу при выходе.
package livetrace

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"

	"gotutor/runner/sandbox"
)

// TraceFile — куда программа пишет трассу. Имя с точкой клиент прислать не
// может (sandbox.Files.Validate), так что подменить его заранее нельзя.
const TraceFile = ".gotutor-trace"

const generatedFile = "gotutor_trace.go"

// Select — оператор select в коде ученика.
type Select struct {
	ID    int    `json:"id"`
	File  string `json:"file"`
	Line  int    `json:"line"`
	Cases []Case `json:"cases"`
	// Counts — сколько раз выбрана каждая ветка за весь запуск.
	Counts []int `json:"counts"`
}

// Case — ветка select: получение, отправка или default.
type Case struct {
	Line int    `json:"line"`
	Kind string `json:"kind"` // recv | send | default
	Text string `json:"text"` // «case v := <-a» без двоеточия
}

type edit struct {
	off  int
	text string
	// cut — сколько байт заменить (переименование main).
	cut int
}

// Instrument возвращает файлы с разметкой и найденные select'ы.
func Instrument(files sandbox.Files) (sandbox.Files, []Select, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasPrefix(name, "gotutor_") {
			return nil, nil, fmt.Errorf("имя файла %s зарезервировано", name)
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			names = append(names, name)
		}
	}
	slices.Sort(names) // id select'ов не должны зависеть от порядка map

	out := make(sandbox.Files, len(files)+1)
	for name, src := range files {
		out[name] = src
	}
	selects := []Select{}
	hasMain := false
	fset := token.NewFileSet()
	for _, name := range names {
		src := files[name]
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			// Ошибку синтаксиса честно покажет go build.
			continue
		}
		if f.Name.Name != "main" {
			continue
		}
		var edits []edit
		off := func(p token.Pos) int { return fset.Position(p).Offset }
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
				edits = append(edits, edit{off: off(fn.Name.Pos()), text: "gotutorMain", cut: len("main")})
				hasMain = true
			}
		}
		// Вставка перед select должна стоять перед меткой, иначе `break L` сломается.
		labeled := map[*ast.SelectStmt]token.Pos{}
		ast.Inspect(f, func(n ast.Node) bool {
			if l, ok := n.(*ast.LabeledStmt); ok {
				inner := l.Stmt
				for {
					next, ok := inner.(*ast.LabeledStmt)
					if !ok {
						break
					}
					inner = next.Stmt
				}
				if sel, ok := inner.(*ast.SelectStmt); ok {
					if _, seen := labeled[sel]; !seen {
						labeled[sel] = l.Pos()
					}
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectStmt)
			if !ok {
				return true
			}
			id := len(selects) + 1
			at, ok := labeled[sel]
			if !ok {
				at = sel.Pos()
			}
			edits = append(edits, edit{off: off(at), text: fmt.Sprintf("gotutorSelect(%d); ", id)})
			s := Select{ID: id, File: name, Line: fset.Position(sel.Pos()).Line}
			for i, st := range sel.Body.List {
				cc := st.(*ast.CommClause)
				c := Case{Line: fset.Position(cc.Pos()).Line, Kind: "default"}
				if cc.Comm != nil {
					c.Kind = "recv"
					if _, ok := cc.Comm.(*ast.SendStmt); ok {
						c.Kind = "send"
					}
				}
				c.Text = strings.TrimSpace(src[off(cc.Pos()):off(cc.Colon)])
				s.Cases = append(s.Cases, c)
				edits = append(edits, edit{off: off(cc.Colon) + 1, text: fmt.Sprintf(" gotutorCase(%d, %d);", id, i)})
			}
			s.Counts = make([]int, len(s.Cases))
			selects = append(selects, s)
			return true
		})
		out[name] = apply(src, edits)
	}
	if !hasMain {
		return nil, nil, errors.New("для визуализации нужна программа с func main в package main")
	}
	out[generatedFile] = generated
	return out, selects, nil
}

// apply вносит правки с конца файла, чтобы смещения оставались верными.
func apply(src string, edits []edit) string {
	slices.SortStableFunc(edits, func(a, b edit) int { return b.off - a.off })
	for _, e := range edits {
		src = src[:e.off] + e.text + src[e.off+e.cut:]
	}
	return src
}

// Импорты с префиксом, чтобы не столкнуться с именами из кода ученика.
const generated = `package main

import (
	gotutorcontext "context"
	gotutoros "os"
	gotutortrace "runtime/trace"
	gotutorstrconv "strconv"
)

func main() {
	if f, err := gotutoros.Create("` + TraceFile + `"); err == nil {
		if gotutortrace.Start(f) == nil {
			defer func() {
				gotutortrace.Stop()
				f.Close()
			}()
		}
	}
	gotutortrace.Log(gotutorcontext.Background(), "gotutor.main", "")
	gotutorMain()
}

func gotutorSelect(id int) {
	gotutortrace.Log(gotutorcontext.Background(), "gotutor.select", gotutorstrconv.Itoa(id))
}

func gotutorCase(id, c int) {
	gotutortrace.Log(gotutorcontext.Background(), "gotutor.case", gotutorstrconv.Itoa(id)+":"+gotutorstrconv.Itoa(c))
}
`

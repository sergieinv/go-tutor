package task

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Vuln — запись локальной базы уязвимостей: версии модуля Module
// в диапазоне [Introduced, Fixed) уязвимы.
type Vuln struct {
	ID         string
	Module     string
	Introduced string // "" — с самой первой версии
	Fixed      string // "" — исправления нет
}

// parsed — версия без «v» и метаданных сборки.
type parsed struct {
	nums [3]int
	pre  []string // nil — релиз
}

func parse(v string) parsed {
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "+") // +incompatible и прочее не сравниваются
	core, pre, hasPre := strings.Cut(v, "-")
	var p parsed
	for i, s := range strings.SplitN(core, ".", 3) {
		p.nums[i], _ = strconv.Atoi(s)
	}
	if hasPre {
		p.pre = strings.Split(pre, ".")
	}
	return p
}

func isNum(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func compareIdent(a, b string) int {
	an, bn := isNum(a), isNum(b)
	switch {
	case an && bn:
		// без ведущих нулей: сначала длина, потом строка
		return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
	case an:
		return -1 // числовой идентификатор меньше буквенного
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

// CompareVersions сравнивает версии модулей Go по правилам semver:
// -1, если a < b; 0, если равны; +1, если a > b.
func CompareVersions(a, b string) int {
	pa, pb := parse(a), parse(b)
	for i := range pa.nums {
		if c := cmp.Compare(pa.nums[i], pb.nums[i]); c != 0 {
			return c
		}
	}
	switch {
	case pa.pre == nil && pb.pre == nil:
		return 0
	case pa.pre == nil:
		return 1 // релиз старше любой своей пре-версии
	case pb.pre == nil:
		return -1
	}
	return slices.CompareFunc(pa.pre, pb.pre, compareIdent)
}

func affected(version string, v Vuln) bool {
	return (v.Introduced == "" || CompareVersions(version, v.Introduced) >= 0) &&
		(v.Fixed == "" || CompareVersions(version, v.Fixed) < 0)
}

// Audit проверяет go.mod по базе уязвимостей и списку разрешённых replace.
func Audit(gomod string, db []Vuln, allowReplace []string) ([]string, error) {
	var findings []string
	block := "" // директива открытого блока «( ... )»
	for n, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		var verb string
		switch {
		case block != "" && f[0] == ")" && len(f) == 1:
			block = ""
			continue
		case block != "":
			verb = block
		case len(f) == 2 && f[1] == "(":
			block = f[0]
			continue
		default:
			verb, f = f[0], f[1:]
		}
		switch verb {
		case "require":
			if len(f) != 2 {
				return nil, fmt.Errorf("go.mod:%d: require: нужны путь и версия", n+1)
			}
			for _, v := range db {
				if v.Module == f[0] && affected(f[1], v) {
					findings = append(findings, v.ID+" "+f[0]+"@"+f[1])
				}
			}
		case "replace":
			arrow := slices.Index(f, "=>")
			if (arrow != 1 && arrow != 2) || len(f)-arrow-1 < 1 || len(f)-arrow-1 > 2 {
				return nil, fmt.Errorf("go.mod:%d: replace: нужно old [v] => new [v]", n+1)
			}
			if !slices.Contains(allowReplace, f[0]) {
				findings = append(findings, "replace "+f[0])
			}
		}
	}
	if block != "" {
		return nil, fmt.Errorf("go.mod: блок %s не закрыт", block)
	}
	slices.Sort(findings)
	return slices.Compact(findings), nil
}

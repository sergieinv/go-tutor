package task

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrBadPlan — вход не похож на вывод EXPLAIN (FORMAT JSON).
var ErrBadPlan = errors.New("bad plan")

// Finding — подозрительный узел плана.
type Finding struct {
	Kind      string  // "seq-scan" или "misestimate"
	Node      string  // "Node Type"
	Relation  string  // "Relation Name", "" если нет
	Estimated float64 // "Plan Rows"
	Actual    float64 // "Actual Rows"
}

// Options — пороги проверок; значение <= 0 отключает проверку.
type Options struct {
	MinSeqScanRows    float64
	MisestimateFactor float64
}

// node — нужные поля узла. Указатели отличают «поля нет»
// (EXPLAIN без ANALYZE) от нуля.
type node struct {
	Type        string   `json:"Node Type"`
	Relation    string   `json:"Relation Name"`
	PlanRows    float64  `json:"Plan Rows"`
	ActualRows  *float64 `json:"Actual Rows"`
	ActualLoops *float64 `json:"Actual Loops"`
	Removed     float64  `json:"Rows Removed by Filter"`
	Plans       []node   `json:"Plans"`
}

// Analyze разбирает план и возвращает находки.
func Analyze(plan []byte, opts Options) ([]Finding, error) {
	var top []struct {
		Plan *node `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &top); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPlan, err)
	}
	if len(top) == 0 || top[0].Plan == nil {
		return nil, fmt.Errorf("%w: нет объекта Plan", ErrBadPlan)
	}
	var out []Finding
	walk(*top[0].Plan, opts, &out)
	return out, nil
}

func walk(n node, opts Options, out *[]Finding) {
	// Фактические числа есть только у EXPLAIN ANALYZE и только
	// у выполнявшихся узлов (never executed — 0 циклов).
	if n.ActualRows != nil && n.ActualLoops != nil && *n.ActualLoops > 0 {
		f := Finding{Node: n.Type, Relation: n.Relation, Estimated: n.PlanRows, Actual: *n.ActualRows}
		// "Actual Rows" и "Rows Removed by Filter" — средние на
		// цикл, всего просмотрено — сумма, умноженная на циклы.
		scanned := (*n.ActualRows + n.Removed) * *n.ActualLoops
		if n.Type == "Seq Scan" && opts.MinSeqScanRows > 0 && scanned >= opts.MinSeqScanRows {
			f.Kind = "seq-scan"
			*out = append(*out, f)
		}
		// Оценка и факт — тоже на один цикл, их и сравниваем.
		lo, hi := min(n.PlanRows, *n.ActualRows), max(n.PlanRows, *n.ActualRows)
		if opts.MisestimateFactor > 0 && hi/max(lo, 1) >= opts.MisestimateFactor {
			f.Kind = "misestimate"
			*out = append(*out, f)
		}
	}
	for _, c := range n.Plans {
		walk(c, opts, out)
	}
}

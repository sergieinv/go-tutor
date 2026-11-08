package task

import "errors"

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

// Analyze разбирает план и возвращает находки.
func Analyze(plan []byte, opts Options) ([]Finding, error) {
	// TODO: encoding/json, обход дерева "Plans" в прямом порядке.
	return nil, nil
}

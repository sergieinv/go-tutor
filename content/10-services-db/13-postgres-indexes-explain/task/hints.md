## Подсказка 1

Опишите узел структурой с тегами `json:"Node Type"`, `json:"Relation Name"`, `json:"Plan Rows"`, `json:"Actual Rows"`, `json:"Actual Loops"`, `json:"Rows Removed by Filter"` и рекурсивным полем `Plans []node`. Числа — `float64`: в PostgreSQL 18 `"Actual Rows": 80.00`, и в `int` такое не разберётся. Чтобы отличить «поля нет» (EXPLAIN без ANALYZE) от нуля, сделайте `Actual Rows` и `Actual Loops` указателями.

## Подсказка 2

Верхний уровень — массив: `var top []struct{ Plan *node }` (тег `json:"Plan"`). Ошибка `json.Unmarshal`, `len(top) == 0` и `top[0].Plan == nil` — всё это `ErrBadPlan`; оберните: `fmt.Errorf("%w: %w", ErrBadPlan, err)`.

## Подсказка 3

Рекурсивный обход: сначала сам узел, потом `Plans` по порядку. Узел проверяется, только если `Actual Rows` и `Actual Loops` есть и циклов больше нуля:

```go
scanned := (*n.ActualRows + n.Removed) * *n.ActualLoops
if n.Type == "Seq Scan" && opts.MinSeqScanRows > 0 && scanned >= opts.MinSeqScanRows { /* seq-scan */ }
lo, hi := min(n.PlanRows, *n.ActualRows), max(n.PlanRows, *n.ActualRows)
if opts.MisestimateFactor > 0 && hi/max(lo, 1) >= opts.MisestimateFactor { /* misestimate */ }
```

## Подсказка 1

Отделите суффикс: если строка оканчивается на `K`, `M` или `G` (`strings.HasSuffix`), число — всё, кроме последнего символа, а множитель — `1<<10`, `1<<20` или `1<<30`. Всё остальное разбирайте как число целиком — `strconv` сам отвергнет лишние символы.

## Подсказка 2

`strconv.ParseInt` пропустит знак `+` и `-`, а `ParseUint` — нет. С `base = 10` подчёркивания и префиксы тоже запрещены. Возьмите `bitSize = 63`: тогда всё, что больше `MaxInt64`, сразу даст `ErrRange`. Ошибку `ParseUint` можно вернуть как есть — это `*strconv.NumError`, и `errors.Is` найдёт в ней `ErrSyntax` или `ErrRange`.

## Подсказка 3

Переполнение умножения проверяйте до умножения:

```go
if int64(n) > math.MaxInt64/mult {
	return 0, &strconv.NumError{Func: "ParseSize", Num: s, Err: strconv.ErrRange}
}
return int64(n) * mult, nil
```

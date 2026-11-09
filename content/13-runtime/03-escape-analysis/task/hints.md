## Подсказка 1

Найдите, что выделяет память: соберите пакет с `go build -gcflags=-m` и ищите `escapes to heap` и `moved to heap`. Подозреваемые — `fmt` (упаковка аргументов в `any`), функции, возвращающие новый срез, вызовы через интерфейс и `fmt.Errorf`.

## Подсказка 2

Для каждого выделения есть замена без кучи: `append` и `strconv.AppendInt` вместо `fmt.Appendf`; `strings.Cut` вместо `strings.SplitN` — он возвращает подстроки исходной строки; `slices.SortFunc` вместо `sort.Sort`; сигнальная ошибка, созданная один раз, вместо `fmt.Errorf` на каждый вызов.

## Подсказка 3

```go
dst = append(dst, user...)
dst = append(dst, ':')
return strconv.AppendInt(dst, int64(id), 10)
```

Для сортировки сравнивайте сначала `cmp.Compare(len(a), len(b))`, при равенстве — `strings.Compare(a, b)`. В `Mean` для пустого среза верните `0, ErrEmpty`.

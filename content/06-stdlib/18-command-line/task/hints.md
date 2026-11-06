## Подсказка 1

Посмотрите на `args[0]` и разберите остаток `args[1:]` отдельным `flag.NewFlagSet` для каждой подкоманды — тогда флаги `add` не будут видны в `list`. Режим — `flag.ContinueOnError`, чтобы `Parse` возвращал ошибку, а не завершал процесс; `fs.SetOutput(io.Discard)` — чтобы ничего не печаталось.

## Подсказка 2

Значения удобно привязывать прямо к полям результата: `fs.IntVar(&cmd.Priority, "p", 1, "...")`. Повторяемый `-tag` — свой тип с методами `String() string` и `Set(string) error`, где `Set` делает `append`; регистрируется через `fs.Var`. Текст — `strings.Join(fs.Args(), " ")`.

## Подсказка 3

```go
type tagList []string

func (t *tagList) String() string     { return strings.Join(*t, ",") }
func (t *tagList) Set(v string) error { *t = append(*t, v); return nil }
```

После `fs.Parse` проверьте диапазоны и `fs.NArg()`. `-h` отдельно обрабатывать не нужно: `Parse` сам вернёт `flag.ErrHelp`. На любой ошибке возвращайте `Command{}`, а не частично заполненную структуру.

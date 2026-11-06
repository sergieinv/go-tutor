---
title: Аргументы командной строки
summary: os.Args, пакет flag — String, Int, Bool, Duration, Var, FlagSet, подкоманды, -h и Usage, позиционные аргументы.
difficulty: 3
minutes: 25
prerequisites:
  - 03-types/03-interfaces
  - 04-errors/02-sentinel-errors
docs:
  - title: "Пакет flag"
    url: https://pkg.go.dev/flag
  - title: "Пакет os: Args"
    url: https://pkg.go.dev/os#pkg-variables
---

`os.Args` — срез строк: `os.Args[0]` — имя программы, дальше аргументы. Разбирает их стандартный пакет `flag`: объявляете флаг (`flag.Int("port", 8080, "описание")` возвращает `*int`), вызываете `Parse`, читаете значения через указатели. Всё, что осталось после флагов, — позиционные аргументы, их отдаёт `Args()`.

Функции `flag.String`, `flag.Parse` и т. д. работают с глобальным набором `flag.CommandLine`, который читает `os.Args[1:]` и при ошибке завершает процесс. Для подкоманд, библиотек и тестов создают свой `flag.NewFlagSet(name, flag.ContinueOnError)` и передают ему срез аргументов явно — тогда `Parse` возвращает ошибку, а не вызывает `os.Exit`.

## Как это в C#

- `string[] args` в `Main` не содержит имени программы; `os.Args` — содержит, аргументы начинаются с `os.Args[1]`.
- `System.CommandLine` (NuGet, стабильная 2.0 с ноября 2025) — дерево команд, типизированные опции, справка и автодополнение. `flag` — минимализм: только флаги, подкоманды собираются вручную из нескольких `FlagSet`. Для сложных CLI в Go берут `spf13/cobra`.
- Синтаксис: `-name value`, `-name=value`, `--name` — одно и то же; одна или две черты равноправны. Объединять короткие флаги (`-abc`) нельзя, а `-name value` не работает для `bool` — только `-v` или `-v=false`.
- Справка `-h`/`-help` строится автоматически из описаний флагов (`PrintDefaults`). Своё оформление задают через поле `Usage`.

## Ловушки

### Разбор останавливается на первом позиционном аргументе

```go
fs.Parse([]string{"input.txt", "-v"})
fmt.Println(*verbose, fs.Args()) // false [input.txt -v]
```

В отличие от GNU getopt, `flag` не переставляет аргументы: всё после первого не-флага (или после `--`) считается позиционным. Флаги пишут перед файлами.

### `-v false` для `bool`

```go
fs.Parse([]string{"-v", "false", "a.txt"})
fmt.Println(*verbose, fs.Args()) // true [false a.txt]
```

Булев флаг не принимает значение через пробел: `-v` уже означает `true`, а `false` становится первым позиционным аргументом — и разбор на нём заканчивается. Пишите `-v=false`.

### `ExitOnError` в библиотеке или тесте

```go
fs := flag.NewFlagSet("tool", flag.ExitOnError)
fs.Parse([]string{"-unknown"}) // печать в stderr и os.Exit(2) — тест просто исчезнет
```

`ExitOnError` (так работает и глобальный `flag.Parse`) уместен только в `main`. В остальном коде — `ContinueOnError`, проверка `errors.Is(err, flag.ErrHelp)` для `-h` и `SetOutput`, чтобы управлять, куда пишутся сообщения.

## Задание

Напишите разбор аргументов CLI-утилиты для списка задач с двумя подкомандами:

```
todo add [-p N] [-tag T]... текст задачи...
todo list [-all] [-limit N]
```

```go
type Command struct {
	Name     string   // "add" или "list"
	Priority int      // add: -p, по умолчанию 1, допустимо 1..5
	Tags     []string // add: -tag, флаг можно повторять
	Text     string   // add: позиционные аргументы через один пробел
	All      bool     // list: -all
	Limit    int      // list: -limit, по умолчанию 10, не меньше 1
}

var ErrNoCommand = errors.New("no command")
var ErrUnknownCommand = errors.New("unknown command")

func Parse(args []string) (Command, error)
```

`args` — аргументы без имени программы (как `os.Args[1:]`). Требования:

- пустой `args` — ошибка `ErrNoCommand`; первый аргумент не `add` и не `list` — ошибка, для которой `errors.Is(err, ErrUnknownCommand)`;
- у каждой подкоманды свой набор флагов: флаг `list` в `add` (и наоборот) — ошибка, как и любой неизвестный флаг или неверное значение;
- `add`: `Tags` в порядке появления (`nil`, если флагов `-tag` нет); `Text` — все позиционные аргументы, соединённые одним пробелом; пустой `Text` — ошибка; `Priority` вне 1..5 — ошибка;
- `list`: позиционные аргументы запрещены — ошибка; `Limit < 1` — ошибка;
- `-h` или `-help` — ошибка, для которой `errors.Is(err, flag.ErrHelp)`;
- при любой ошибке возвращается нулевой `Command{}`;
- `Parse` ничего не печатает (ни в stdout, ни в stderr) и не завершает процесс.

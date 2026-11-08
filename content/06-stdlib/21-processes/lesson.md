---
title: Запуск процессов
summary: os/exec — Command, Output, CombinedOutput, Run и *exec.ExitError, stdin и stdout через пайпы, CommandContext с таймаутом и запуск без оболочки.
difficulty: 3
minutes: 25
prerequisites:
  - 06-stdlib/12-reading-files
  - 05-concurrency/18-context
  - 04-errors/04-wrapping
docs:
  - title: "Пакет os/exec"
    url: https://pkg.go.dev/os/exec
  - title: "Пакет os/exec: ExitError"
    url: https://pkg.go.dev/os/exec#ExitError
---

`exec.Command(name, args...)` описывает запуск программы: путь или имя для поиска в `PATH` и аргументы отдельными строками. До запуска можно настроить `Dir`, `Env`, `Stdin`, `Stdout`, `Stderr`. Затем — `Run` (запустить и дождаться), `Output` (то же и вернуть stdout) или `CombinedOutput` (stdout и stderr вместе). Для параллельной работы с процессом — `Start` и `Wait`, для чтения вывода по мере появления — `StdoutPipe`.

Оболочка не участвует: аргументы попадают в программу как есть, без раскрытия `$VAR`, `*` и кавычек. Это защищает от инъекций — имя файла `"; rm -rf /"` останется именем файла. Если нужны конвейеры и перенаправления, оболочку вызывают явно: `exec.Command("/bin/sh", "-c", script)`. Внешний ввод в `script` не подставляйте — это снова инъекция; передайте его отдельным аргументом: `` exec.Command("/bin/sh", "-c", `grep -- "$1" log.txt`, "sh", pattern) ``. И без оболочки значение, начинающееся с `-`, программа примет за флаг — отделяйте такие аргументы `--`, если программа это поддерживает.

## Как это в C#

- `Process.Start(new ProcessStartInfo { FileName, ArgumentList, RedirectStandardOutput = true })` → `exec.Command(name, args...)` + `cmd.Stdout = &buf`. Аналог `ArgumentList` — передача аргументов отдельными строками; единой строки `Arguments`, которую надо экранировать, в Go нет.
- `UseShellExecute` в Go нет вообще: `exec` всегда запускает программу напрямую.
- `process.ExitCode` читают после `WaitForExit`. В Go ненулевой код — это ошибка `*exec.ExitError`, код достают через `errors.As` и `ExitCode()`.
- `WaitForExitAsync(cancellationToken)` при отмене только перестаёт ждать — процесс продолжает работу, убивать его надо самому через `Kill`. `exec.CommandContext` по отмене контекста убивает процесс сам.

## Ловушки

### Строка команды целиком

```go
exec.Command("ls -la /tmp").Run() // exec: "ls -la /tmp": executable file not found
exec.Command("ls", "-la", "/tmp").Run() // правильно
```

Первый аргумент — имя программы, а не командная строка. Делить строку пробелами (`strings.Fields`) тоже опасно — сломаются аргументы с пробелами. Передавайте аргументы срезом.

### Таймаут не виден в ошибке

```go
err := exec.CommandContext(ctx, "convert", in, out).Run()
// err: "signal: killed" — *exec.ExitError, а не context.DeadlineExceeded
```

Когда контекст истекает, процесс убивается, и `Wait` возвращает ошибку убитого процесса. Чтобы понять, что виноват таймаут, проверяйте `ctx.Err()` после `Run`.

### Внук держит пайп

```go
cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 100 & echo started")
out, err := cmd.Output() // висит, пока жив sleep, — даже после отмены ctx
```

`Output` ждёт закрытия stdout, а его унаследовал фоновый `sleep`. Убийство `sh` по таймауту не помогает — пайп открыт у внука. Задайте `cmd.WaitDelay`: через это время после убийства `Wait` закроет пайпы сам.

## Задание

Напишите обёртку для запуска команды с захватом вывода:

```go
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func Run(ctx context.Context, stdin string, name string, args ...string) (Result, error)
```

Требования:

- программа `name` запускается напрямую с аргументами `args`, без оболочки; на её stdin подаётся строка `stdin`; stdout и stderr собираются раздельно;
- если программа завершилась (с любым кодом, в том числе ненулевым), `Run` возвращает заполненный `Result` с её кодом выхода и `nil`-ошибку — ненулевой код не считается ошибкой `Run`;
- если программу убил сигнал не из-за `ctx` (например, она сама получила `SIGKILL`), это тоже не ошибка `Run`: заполненный `Result` с `ExitCode` −1 (как у `ExitError.ExitCode()`) и `nil`-ошибка;
- если программу не удалось запустить (например, её нет в `PATH`), возвращается ошибка, для которой `errors.Is` находит исходную (`exec.ErrNotFound` для отсутствующей программы); `Result` в этом случае не важен;
- если `ctx` отменён или истёк до завершения программы, процесс убивается, а `Run` возвращает ошибку, для которой `errors.Is(err, ctx.Err())` истинно (`context.DeadlineExceeded` или `context.Canceled`); `Result` в этом случае не важен.

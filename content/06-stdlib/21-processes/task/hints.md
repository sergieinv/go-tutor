## Подсказка 1

`exec.CommandContext(ctx, name, args...)` запускает программу без оболочки и убьёт её при отмене `ctx`. Вход — `cmd.Stdin = strings.NewReader(stdin)`, выходы — два разных `strings.Builder` в `cmd.Stdout` и `cmd.Stderr`. Не используйте `StdoutPipe`: с ним легко зависнуть, если читать потоки по очереди.

## Подсказка 2

После `cmd.Run()` разберите ошибку. Если `ctx.Err() != nil` — процесс убит из-за контекста, хотя сама ошибка выглядит как «signal: killed»; оберните `ctx.Err()` через `%w`. Ненулевой код выхода — `*exec.ExitError`, его находит `errors.As`.

## Подсказка 3

```go
err := cmd.Run()
if err != nil && ctx.Err() != nil {
	return Result{}, fmt.Errorf("%s: %w", name, ctx.Err())
}
res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
var exitErr *exec.ExitError
if errors.As(err, &exitErr) {
	res.ExitCode = exitErr.ExitCode()
	return res, nil
}
```

Остаётся: `err == nil` → `res, nil`; любая другая ошибка — программа не запустилась, верните её. Программа, убитая сигналом, тоже даёт `*exec.ExitError`, и `ExitCode()` у неё −1.

Одна тонкость: когда `cmd.Stdout` — не файл, `exec` читает вывод через трубу, и `Wait` ждёт, пока трубу закроют все, кто её держит. Если программа запустила фоновый процесс-потомок и тот унаследовал stdout, `Run` будет висеть даже после убийства программы по `ctx`. `cmd.WaitDelay` ограничивает это ожидание: через заданное время после отмены `ctx` или выхода программы `exec` сам закроет трубы.

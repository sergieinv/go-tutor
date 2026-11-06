---
title: Сигналы и завершение процесса
summary: os/signal — Notify и NotifyContext, graceful shutdown, коды выхода, os.Exit без defer и log.Fatal.
difficulty: 3
minutes: 25
prerequisites:
  - 05-concurrency/18-context
  - 04-errors/06-panic-recover
  - 06-stdlib/21-processes
docs:
  - title: "Пакет os/signal"
    url: https://pkg.go.dev/os/signal
  - title: "Пакет os/signal: NotifyContext"
    url: https://pkg.go.dev/os/signal#NotifyContext
  - title: "Пакет os: Exit"
    url: https://pkg.go.dev/os#Exit
---

По умолчанию Go-программа на `SIGINT` (Ctrl+C) и `SIGTERM` (`docker stop`, `kill`, systemd) завершается сразу — без `defer`, без закрытия соединений и сброса буферов. Чтобы остановиться аккуратно, сигнал перехватывают: `signal.Notify(ch, sigs...)` отправляет его в канал, а `signal.NotifyContext(ctx, sigs...)` отменяет контекст. Дальше — обычная отмена: горутины видят `ctx.Done()`, дописывают работу, `main` ждёт их с ограничением по времени и возвращается.

Код выхода задаёт `os.Exit(code)`: `0` — успех, всё остальное — ошибка. `os.Exit` завершает процесс немедленно — **отложенные вызовы не выполняются**. Так же ведут себя `log.Fatal` (это `os.Exit(1)`) и `flag.ExitOnError` (`os.Exit(2)`). Необработанная паника, наоборот, раскручивает стек с `defer` и завершает процесс с кодом 2.

## Как это в C#

- `Console.CancelKeyPress` (Ctrl+C), `AppDomain.ProcessExit` (срабатывает уже при завершении, в том числе по SIGTERM) и `PosixSignalRegistration` (.NET 6+) — колбэки, которые выполняются в отдельном потоке. В Go сигнал приходит в канал или отменяет контекст, обработку пишете в обычном потоке управления.
- Generic Host в .NET сам слушает сигналы и отменяет `stoppingToken` у `BackgroundService`. `signal.NotifyContext` — то же самое в одну строку, но остальное (ожидание горутин, таймаут остановки) вы собираете сами.
- `Environment.Exit(code)` вызывает обработчики `ProcessExit`; `os.Exit` не выполняет ничего — ни `defer`, ни сброса буферов `bufio.Writer`.
- `Main` в C# может вернуть `int`. `func main()` в Go ничего не возвращает: код выхода, отличный от 0, — только через `os.Exit`. Обычный приём — вся логика в `run() error`, а `main` лишь вызывает `os.Exit(1)` при ошибке.

## Ловушки

### `os.Exit` и `log.Fatal` пропускают `defer`

```go
func main() {
	f, _ := os.Create("out.txt")
	w := bufio.NewWriter(f)
	defer w.Flush() // не выполнится
	if err := work(w); err != nil {
		log.Fatal(err) // os.Exit(1): буфер пропал, файл пустой
	}
}
```

Вызывайте `os.Exit` только в `main` и только после того, как отработала вся логика: `if err := run(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }` — все `defer` внутри `run` уже выполнились.

### Небуферизованный канал для `Notify`

```go
ch := make(chan os.Signal) // без буфера
signal.Notify(ch, os.Interrupt)
```

Пакет `signal` отправляет в канал неблокирующе: если в этот момент никто не читает, сигнал теряется. Канал должен иметь буфер хотя бы на один элемент — `go vet` об этом предупреждает.

### Повторный Ctrl+C

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
<-ctx.Done()
shutdown() // зависла — второй Ctrl+C тоже перехвачен и ничего не делает
```

Пока подписка активна, сигналы не завершают процесс. Вызовите `stop()` сразу после `<-ctx.Done()`: тогда второй Ctrl+C снова сработает по умолчанию и убьёт зависшую программу.

## Задание

Напишите обёртку для `main`, которая превращает работу программы в код выхода:

```go
func Main(run func(ctx context.Context) error, stderr io.Writer, sigs ...os.Signal) int
```

`main` тогда выглядит как `os.Exit(Main(run, os.Stderr))`. Требования:

- `run` вызывается с контекстом, который отменяется при получении любого из сигналов `sigs`; если `sigs` пуст — `os.Interrupt` и `syscall.SIGTERM`;
- подписка на сигналы снимается, когда `Main` возвращается;
- `run` вернула `nil` → код `0`, в `stderr` ничего не пишется;
- сигнал пришёл, и `run` вернула ошибку, для которой `errors.Is(err, context.Canceled)`, — это штатная остановка: код `0`, в `stderr` ничего не пишется;
- любая другая ошибка (в том числе `context.Canceled` без сигнала) → код `1`, в `stderr` пишется строка `error: <текст ошибки>\n`;
- если `run` паникует, паника перехватывается: код `2`, в `stderr` — строка `panic: <значение>\n` (значение форматируется как `%v`);
- `Main` сама не вызывает `os.Exit`.

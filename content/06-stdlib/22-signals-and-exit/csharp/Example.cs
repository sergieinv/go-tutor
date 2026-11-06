using System.Runtime.InteropServices;

using var cts = new CancellationTokenSource();

// Ctrl+C (SIGINT) — событие; обработчик выполняется в отдельном потоке.
// В Go: signal.NotifyContext(ctx, os.Interrupt) отменяет контекст.
Console.CancelKeyPress += (_, e) =>
{
    e.Cancel = true; // не завершать процесс сразу
    cts.Cancel();
};

// SIGTERM (docker stop): в .NET 6+ — PosixSignalRegistration. ctx.Cancel = true
// отменяет завершение по умолчанию, и процесс выходит сам, как после return из Main.
// AppDomain.ProcessExit для этого не годится: он срабатывает, когда процесс уже
// завершается (в том числе после return из Main, когда cts уже освобождён).
using var term = PosixSignalRegistration.Create(PosixSignal.SIGTERM, ctx =>
{
    ctx.Cancel = true;
    cts.Cancel();
});

try
{
    await Task.Delay(Timeout.Infinite, cts.Token); // «работа» до сигнала
}
catch (OperationCanceledException)
{
    Console.WriteLine("остановка: сохраняю состояние");
}

// Environment.Exit вызывает обработчики ProcessExit; os.Exit в Go не выполняет даже defer.
// Main может вернуть int — в Go код выхода задаёт только os.Exit.
return 0;

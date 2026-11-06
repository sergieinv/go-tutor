// Одноразовая задержка с отменой — аналог Timer + Stop
using var cts = new CancellationTokenSource();
var delay = Task.Delay(TimeSpan.FromHours(1), cts.Token);
cts.Cancel(); // как t.Stop()

// Callback по таймеру в пуле потоков — аналог time.AfterFunc
var done = new TaskCompletionSource<string>();
using var timer = new Timer(_ => done.SetResult("AfterFunc выполнен"), null, 10, Timeout.Infinite);
Console.WriteLine(await done.Task);
// timer.Change(20, Timeout.Infinite) — аналог Reset

// Периодическая работа — PeriodicTimer вместо time.Ticker
using var ticker = new PeriodicTimer(TimeSpan.FromMilliseconds(10));
for (var i = 1; i <= 3; i++)
{
    await ticker.WaitForNextTickAsync();
    Console.WriteLine($"тик {i}");
}
// Dispose останавливает тикер, и WaitForNextTickAsync вернёт false.
// В Go Stop канал не закрывает.

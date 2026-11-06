using System.Threading.Channels;

// Таймаут без отмены: Task.WhenAny вернулся, а работа продолжается.
// Задача не «утекает» навсегда — она доработает и будет собрана GC.
var work = Task.Run(async () => { await Task.Delay(50); return 42; });
if (await Task.WhenAny(work, Task.Delay(1)) != work)
    Console.WriteLine("таймаут");

// Аналог утечки горутины: запись в канал без читателя ждёт вечно,
// и задача висит вместе со всем, что захватила, — пока канал достижим.
// Когда ссылок на канал не останется, GC соберёт и задачу; горутину — нет.
var ch = Channel.CreateBounded<int>(1);
_ = Task.Run(async () =>
{
    for (var i = 0; ; i++)
        await ch.Writer.WriteAsync(i); // после первой записи — вечное ожидание
});

// Исправление — токен отмены, как ctx.Done() в Go.
using var cts = new CancellationTokenSource();
var poller = Task.Run(async () =>
{
    using var timer = new PeriodicTimer(TimeSpan.FromMilliseconds(1));
    try
    {
        while (await timer.WaitForNextTickAsync(cts.Token)) { }
    }
    catch (OperationCanceledException) { }
});
cts.Cancel();
await poller;
Console.WriteLine("poller остановлен");

using System.Threading.Channels;

// Bounded-канал с режимом Wait — аналог make(chan string, 3)
var queue = Channel.CreateBounded<string>(new BoundedChannelOptions(3)
{
    FullMode = BoundedChannelFullMode.Wait, // запись ждёт свободного места
});
await queue.Writer.WriteAsync("a");
await queue.Writer.WriteAsync("b");
Console.WriteLine($"count: {queue.Reader.Count}"); // len(ch); cap(ch) отдельно не узнать

// В .NET есть и бесконечная очередь — в Go такой нет.
var unbounded = Channel.CreateUnbounded<int>();

// Семафор — отдельный тип. В Go его роль играет chan struct{} на N.
var sem = new SemaphoreSlim(3);
var running = 0;
var exceeded = false;
var tasks = Enumerable.Range(0, 20).Select(async _ =>
{
    await sem.WaitAsync(); // sem <- struct{}{}
    try
    {
        if (Interlocked.Increment(ref running) > 3) exceeded = true;
        Interlocked.Decrement(ref running);
    }
    finally
    {
        sem.Release(); // <-sem
    }
});
await Task.WhenAll(tasks);
Console.WriteLine($"лимит 3 превышен: {exceeded}");

using System.Threading.Channels;

var ch = Channel.CreateUnbounded<int>();

// Производитель завершает канал — аналог close(ch).
_ = Task.Run(async () =>
{
    for (var i = 1; i <= 3; i++)
        await ch.Writer.WriteAsync(i * 10);
    ch.Writer.Complete();
});

// await foreach — аналог for v := range ch: заканчивается после Complete.
await foreach (var v in ch.Reader.ReadAllAsync())
    Console.WriteLine($"range: {v}");

// Запись в завершённый канал — исключение, которое можно поймать.
// В Go это паника: send on closed channel.
try
{
    await ch.Writer.WriteAsync(1);
}
catch (ChannelClosedException)
{
    Console.WriteLine("канал завершён");
}

// Повторное завершение не паникует, если использовать TryComplete.
Console.WriteLine(ch.Writer.TryComplete()); // False

// Широковещательный сигнал — не канал, а токен отмены или TCS.
var start = new TaskCompletionSource();
var runners = Enumerable.Range(0, 3).Select(async i =>
{
    await start.Task; // как <-start
    Console.WriteLine($"бегун {i} стартовал");
});
start.SetResult(); // как close(start): будит всех
await Task.WhenAll(runners);

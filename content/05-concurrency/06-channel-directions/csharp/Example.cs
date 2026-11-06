using System.Threading.Channels;

// Генератор: наружу отдаём только ChannelReader — как <-chan int.
static ChannelReader<int> Count(int from, int to)
{
    var ch = Channel.CreateUnbounded<int>();
    _ = Task.Run(() =>
    {
        for (var i = from; i < to; i++) ch.Writer.TryWrite(i);
        ch.Writer.Complete(); // закрыть может только владелец Writer
    });
    return ch.Reader;
}

// Стадия: читает из одного Reader, отдаёт свой.
static ChannelReader<int> Square(ChannelReader<int> input)
{
    var ch = Channel.CreateUnbounded<int>();
    _ = Task.Run(async () =>
    {
        await foreach (var v in input.ReadAllAsync())
            await ch.Writer.WriteAsync(v * v);
        ch.Writer.Complete();
    });
    return ch.Reader;
}

// Только запись — как chan<- string.
static void Report(int sum, ChannelWriter<string> log) =>
    log.TryWrite($"сумма квадратов: {sum}");

var sum = 0;
await foreach (var v in Square(Count(1, 5)).ReadAllAsync())
{
    Console.WriteLine($"квадрат: {v}");
    sum += v;
}

var log = Channel.CreateUnbounded<string>();
Report(sum, log.Writer);
Console.WriteLine(await log.Reader.ReadAsync());

// Ленивая альтернатива без потоков — IAsyncEnumerable с yield.
static async IAsyncEnumerable<int> CountLazy(int from, int to)
{
    for (var i = from; i < to; i++)
    {
        await Task.Yield();
        yield return i;
    }
}
await foreach (var v in CountLazy(0, 2)) Console.WriteLine(v);

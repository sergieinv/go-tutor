using System.Threading.Channels;
using System.Threading.Tasks.Dataflow;

// Pipeline на System.Threading.Channels: стадия — задача,
// которая читает один канал и пишет в другой.
static ChannelReader<int> Square(ChannelReader<int> input, CancellationToken ct)
{
    var output = Channel.CreateBounded<int>(1); // ограниченный буфер — backpressure
    _ = Task.Run(async () =>
    {
        try
        {
            await foreach (var n in input.ReadAllAsync(ct))
                await output.Writer.WriteAsync(n * n, ct);
        }
        finally
        {
            output.Writer.Complete(); // аналог close(out)
        }
    }, ct);
    return output.Reader;
}

using var cts = new CancellationTokenSource();
var source = Channel.CreateUnbounded<int>();
foreach (var n in new[] { 1, 2, 3 }) source.Writer.TryWrite(n);
source.Writer.Complete();

await foreach (var n in Square(source.Reader, cts.Token).ReadAllAsync())
    Console.WriteLine(n);

// TPL Dataflow: fan-out — MaxDegreeOfParallelism, backpressure — BoundedCapacity.
var square = new TransformBlock<int, int>(n => n * n,
    new ExecutionDataflowBlockOptions { MaxDegreeOfParallelism = 3, BoundedCapacity = 4 });
var print = new ActionBlock<int>(Console.WriteLine);
square.LinkTo(print, new DataflowLinkOptions { PropagateCompletion = true });
for (var i = 1; i <= 6; i++) await square.SendAsync(i);
square.Complete();
await print.Completion;

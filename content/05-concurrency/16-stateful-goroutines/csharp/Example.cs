using System.Threading.Channels;

// Актор на Channel<T>: одна задача читает почтовый ящик и владеет словарём
var counters = new Counters();
await Task.WhenAll(Enumerable.Range(0, 10).Select(i => Task.Run(async () =>
{
    var key = i % 2 == 0 ? "чёт" : "нечет";
    for (var n = 0; n < 100; n++) await counters.AddAsync(key, 1);
})));
Console.WriteLine($"чёт: {await counters.GetAsync("чёт")} нечет: {await counters.GetAsync("нечет")}");
counters.Complete();

abstract record Op;
// TaskCompletionSource — это канал ответа resp из Go
record Read(string Key, TaskCompletionSource<int> Resp) : Op;
record Add(string Key, int Delta, TaskCompletionSource Resp) : Op;

class Counters
{
    private readonly Channel<Op> _inbox = Channel.CreateUnbounded<Op>();

    public Counters() => _ = Task.Run(Loop);

    private async Task Loop()
    {
        var state = new Dictionary<string, int>(); // видна только этой задаче
        await foreach (var op in _inbox.Reader.ReadAllAsync())
        {
            switch (op)
            {
                case Read r: r.Resp.SetResult(state.GetValueOrDefault(r.Key)); break;
                case Add a:
                    state[a.Key] = state.GetValueOrDefault(a.Key) + a.Delta;
                    a.Resp.SetResult();
                    break;
            }
        }
    }

    public Task AddAsync(string key, int delta)
    {
        var tcs = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        _inbox.Writer.TryWrite(new Add(key, delta, tcs));
        return tcs.Task;
    }

    public Task<int> GetAsync(string key)
    {
        var tcs = new TaskCompletionSource<int>(TaskCreationOptions.RunContinuationsAsynchronously);
        _inbox.Writer.TryWrite(new Read(key, tcs));
        return tcs.Task;
    }

    public void Complete() => _inbox.Writer.Complete(); // аналог close(quit)
}

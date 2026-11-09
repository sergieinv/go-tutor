using System.Diagnostics;

// Открытая модель в C#: запрос уходит по расписанию, не дожидаясь
// предыдущих (Task без await в цикле). NBomber делает то же самое
// через Simulation.Inject(rate, interval, during).
var gate = new SemaphoreSlim(1, 1);           // «медленный сервер»: один запрос за раз
async Task Handle()
{
    await gate.WaitAsync();
    try { await Task.Delay(10); } finally { gate.Release(); }
}

const int N = 30;
var interval = TimeSpan.FromMilliseconds(2);
var latencies = new TimeSpan[N];
var start = Stopwatch.GetTimestamp();
var tasks = new List<Task>();
for (int i = 0; i < N; i++)
{
    var intended = start + (long)(i * interval.TotalSeconds * Stopwatch.Frequency);
    var wait = Stopwatch.GetElapsedTime(Stopwatch.GetTimestamp(), intended);
    if (wait > TimeSpan.Zero) await Task.Delay(wait);
    int idx = i;
    tasks.Add(Task.Run(async () =>
    {
        await Handle();
        // Задержка — от запланированного момента, а не от отправки.
        latencies[idx] = Stopwatch.GetElapsedTime(intended);
    }));
}
await Task.WhenAll(tasks);
Array.Sort(latencies);

// Перцентиль методом ближайшего ранга — как в примере на Go.
static TimeSpan Percentile(TimeSpan[] sorted, double p)
{
    int k = (int)Math.Ceiling(p / 100 * sorted.Length);
    return sorted[Math.Clamp(k, 1, sorted.Length) - 1];
}
Console.WriteLine($"отправлено {N}");
Console.WriteLine($"p99 > p50: {Percentile(latencies, 99) > Percentile(latencies, 50)}");

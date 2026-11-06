using System.Collections.Concurrent;

var words = new[] { "go", "channel", "worker", "pool", "select", "mutex" };

// Пул с лимитом параллелизма — одной строкой
var upper = new string[words.Length];
await Parallel.ForEachAsync(
    Enumerable.Range(0, words.Length),
    new ParallelOptions { MaxDegreeOfParallelism = 3 },
    async (i, ct) =>
    {
        await Task.Yield();
        upper[i] = words[i].ToUpperInvariant(); // свой элемент массива — как out[i] в Go
    });
Console.WriteLine(string.Join(" ", upper));

// Семафор вместо буферизованного канала
var sem = new SemaphoreSlim(2);
var lengths = await Task.WhenAll(words.Select(async w =>
{
    await sem.WaitAsync();   // sem <- struct{}{}
    try { return w.Length; }
    finally { sem.Release(); } // <-sem
}));
Console.WriteLine(string.Join(" ", lengths)); // WhenAll сохраняет порядок входа

// Классический пул «производитель — потребители» на BlockingCollection
var jobs = new BlockingCollection<string>();
var results = new ConcurrentBag<string>(); // порядок не гарантирован
var workers = Enumerable.Range(0, 3).Select(_ => Task.Run(() =>
{
    foreach (var w in jobs.GetConsumingEnumerable()) results.Add(w.ToUpperInvariant());
})).ToArray();
foreach (var w in words) jobs.Add(w);
jobs.CompleteAdding(); // аналог close(jobs)
await Task.WhenAll(workers);

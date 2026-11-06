var urls = new[] { "a.com", "bad.com", "c.com" };

// Связанный источник отмены — errgroup.WithContext делает это сам
using var cts = CancellationTokenSource.CreateLinkedTokenSource(CancellationToken.None);
var tasks = urls.Select(async u =>
{
    try
    {
        return await Fetch(u, TimeSpan.FromSeconds(1), cts.Token);
    }
    catch (Exception e) when (e is not OperationCanceledException)
    {
        cts.Cancel(); // первая ошибка отменяет соседей — вручную
        throw;
    }
}).ToArray();

var all = Task.WhenAll(tasks);
try
{
    await all; // await бросает первое исключение
}
catch (Exception e)
{
    Console.WriteLine($"ошибка: {e.Message}");
    // Все ошибки — в AggregateException; errgroup.Wait вернул бы только первую
    Console.WriteLine($"всего исключений: {all.Exception!.InnerExceptions.Count}");
}

// Ограничение параллелизма — как errgroup.SetLimit(2)
await Parallel.ForEachAsync(urls.Where(u => !u.Contains("bad")),
    new ParallelOptions { MaxDegreeOfParallelism = 2 },
    async (u, ct) => await Fetch(u, TimeSpan.FromMilliseconds(10), ct));

static async Task<string> Fetch(string url, TimeSpan d, CancellationToken ct)
{
    if (url.Contains("bad")) throw new HttpRequestException($"fetch {url}: 500");
    await Task.Delay(d, ct);
    return "<html>" + url;
}

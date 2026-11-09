// В .NET 9+ защиту от stampede даёт HybridCache: GetOrCreateAsync вызывает
// фабрику один раз на ключ, остальные вызовы ждут её результат.
//
//   builder.Services.AddHybridCache(); // пакет Microsoft.Extensions.Caching.Hybrid
//   var card = await hybrid.GetOrCreateAsync($"product:{sku}",
//       async ct => await db.LoadAsync(sku, ct),
//       new HybridCacheEntryOptions { Expiration = TimeSpan.FromMinutes(1) },
//       cancellationToken: ct);
//
// IMemoryCache.GetOrCreate такой защиты не даёт. Без HybridCache пишут
// свой singleflight: ConcurrentDictionary<string, Lazy<Task<T>>> — то же,
// что singleflight.Group в Go.
using System.Collections.Concurrent;

public sealed class SingleFlight<T>
{
    readonly ConcurrentDictionary<string, Lazy<Task<T>>> _inflight = new();

    public async Task<T> DoAsync(string key, Func<Task<T>> load)
    {
        // Lazy гарантирует один вызов load на ключ, даже если GetOrAdd
        // создаст несколько Lazy при гонке, — в словарь попадёт один.
        var lazy = _inflight.GetOrAdd(key, _ => new Lazy<Task<T>>(load));
        try
        {
            return await lazy.Value; // ошибка тоже общая: исключение у всех
        }
        finally
        {
            // Забыть ключ — следующая загрузка пойдёт заново (как Forget).
            _inflight.TryRemove(new KeyValuePair<string, Lazy<Task<T>>>(key, lazy));
        }
    }
}

public static class Program
{
    public static async Task Main()
    {
        var sf = new SingleFlight<string>();
        var loads = 0;
        var gate = new TaskCompletionSource();
        async Task<string> Load()
        {
            Interlocked.Increment(ref loads);
            await gate.Task; // «долгий» запрос в базу
            return "карточка apple";
        }

        var requests = Enumerable.Range(0, 100).Select(_ => sf.DoAsync("apple", Load)).ToArray();
        gate.SetResult();
        await Task.WhenAll(requests);
        Console.WriteLine($"запросов 100, загрузок {loads}"); // 1

        // Отмена ожидания одного вызова без отмены общей загрузки —
        // WaitAsync(ct) поверх общего Task, как DoChan + select в Go.
        using var cts = new CancellationTokenSource(TimeSpan.FromMilliseconds(10));
        try { await sf.DoAsync("pear", () => Task.Delay(1000).ContinueWith(_ => "x")).WaitAsync(cts.Token); }
        catch (OperationCanceledException) { Console.WriteLine("ждущий ушёл по отмене"); }
    }
}

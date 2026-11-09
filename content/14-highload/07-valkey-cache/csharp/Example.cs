// Распределённый кеш в ASP.NET Core — IDistributedCache. С Redis/Valkey его
// подключают пакетом Microsoft.Extensions.Caching.StackExchangeRedis:
//
//   builder.Services.AddStackExchangeRedisCache(o =>
//   {
//       o.Configuration = "valkey:6379,user=orders,password=...";
//       o.InstanceName = "orders:"; // префикс ключей, как VALKEY_PREFIX
//   });
//
// Здесь — MemoryDistributedCache из коробки, чтобы файл собирался без NuGet.
using System.Text.Json;
using Microsoft.Extensions.Caching.Distributed;
using Microsoft.Extensions.Caching.Memory;
using Microsoft.Extensions.Options;

public sealed record Product(long Id, string Name, long Price);

public interface IProductStore
{
    Task<Product?> FindAsync(long id, CancellationToken ct);
    Task SaveAsync(Product p, CancellationToken ct);
}

public sealed class Catalog(IDistributedCache cache, IProductStore store)
{
    static string Key(long id) => $"product:v1:{id}";

    public async Task<Product?> GetAsync(long id, CancellationToken ct)
    {
        string? cached = null;
        try
        {
            cached = await cache.GetStringAsync(Key(id), ct);
        }
        catch (Exception) when (!ct.IsCancellationRequested)
        {
            return await store.FindAsync(id, ct); // кеш недоступен — в базу
        }
        if (cached is not null)
            return cached.Length == 0 ? null : JsonSerializer.Deserialize<Product>(cached);

        var p = await store.FindAsync(id, ct);
        // Отрицательная запись — пустая строка с коротким TTL; TTL товара — с джиттером.
        var ttl = p is null
            ? TimeSpan.FromSeconds(10)
            : TimeSpan.FromMinutes(1) + TimeSpan.FromMilliseconds(Random.Shared.Next(6_000));
        await cache.SetStringAsync(Key(id), p is null ? "" : JsonSerializer.Serialize(p),
            new DistributedCacheEntryOptions { AbsoluteExpirationRelativeToNow = ttl }, ct);
        return p;
    }

    public async Task SaveAsync(Product p, CancellationToken ct)
    {
        await store.SaveAsync(p, ct);
        await cache.RemoveAsync(Key(p.Id), ct); // инвалидация после записи
    }
}

sealed class MemoryStore : IProductStore
{
    readonly Dictionary<long, Product> _rows = new() { [1] = new Product(1, "яблоки", 120) };
    public int Reads;
    public Task<Product?> FindAsync(long id, CancellationToken ct)
    {
        Reads++;
        return Task.FromResult(_rows.GetValueOrDefault(id));
    }
    public Task SaveAsync(Product p, CancellationToken ct)
    {
        _rows[p.Id] = p;
        return Task.CompletedTask;
    }
}

public static class Program
{
    public static async Task Main()
    {
        var cache = new MemoryDistributedCache(Options.Create(new MemoryDistributedCacheOptions()));
        var store = new MemoryStore();
        var catalog = new Catalog(cache, store);
        await catalog.GetAsync(1, default);
        await catalog.GetAsync(1, default);
        await catalog.GetAsync(404, default);
        await catalog.GetAsync(404, default);
        Console.WriteLine($"чтений базы: {store.Reads}"); // 2
    }
}

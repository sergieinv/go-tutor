// IMemoryCache — кеш в памяти процесса из Microsoft.Extensions.Caching.Memory.
// В Go стандартного кеша нет: map + мьютекс, container/list для LRU
// или библиотека (ristretto, otter, hashicorp/golang-lru).
using Microsoft.Extensions.Caching.Memory;

public sealed record Product(string Sku, decimal Price);

public sealed class Catalog(IMemoryCache cache, Func<string, Product> load)
{
    public Product Get(string sku) =>
        // GetOrCreate не защищает от stampede: при промахе фабрику могут
        // вызвать несколько потоков сразу (об этом — урок 08).
        cache.GetOrCreate(sku, entry =>
        {
            entry.AbsoluteExpirationRelativeToNow = TimeSpan.FromMinutes(5); // TTL
            entry.SlidingExpiration = TimeSpan.FromMinutes(1); // продлевается чтением
            entry.Size = 1; // обязателен, если у кеша задан SizeLimit
            return load(sku);
        })!;
}

public static class Program
{
    public static void Main()
    {
        // SizeLimit — в условных единицах Size. Запись, которая не влезает,
        // просто не добавляется (а не вытесняет LRU), и в фоне запускается
        // компактизация. Истёкшие записи ищутся не таймером, а при обращениях
        // к кешу, не чаще ExpirationScanFrequency (по умолчанию 1 мин).
        IMemoryCache cache = new MemoryCache(new MemoryCacheOptions { SizeLimit = 10_000 });
        var catalog = new Catalog(cache, sku => new Product(sku, 120m));
        Console.WriteLine(catalog.Get("apple"));
    }
}

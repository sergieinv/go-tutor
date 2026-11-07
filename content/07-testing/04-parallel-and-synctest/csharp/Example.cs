// .NET 8+: код получает TimeProvider, тест подсовывает фейк и двигает время сам.
// Обычно берут FakeTimeProvider из NuGet Microsoft.Extensions.TimeProvider.Testing;
// чтобы файл собирался без NuGet, ниже минимальный фейк только для GetUtcNow.
var clock = new ManualTimeProvider(new DateTimeOffset(2000, 1, 1, 0, 0, 0, TimeSpan.Zero));
var cache = new TtlCache(TimeSpan.FromMinutes(1), clock);
cache.Set("k", "v");

clock.Advance(TimeSpan.FromMinutes(1) - TimeSpan.FromTicks(1));
Console.WriteLine(cache.TryGet("k", out _)); // True

// В Go synctest двигает часы сам, когда все горутины пузыря ждут;
// здесь время идёт только по явному Advance.
clock.Advance(TimeSpan.FromTicks(1));
Console.WriteLine(cache.TryGet("k", out _)); // False

// Чтобы тестировать код со временем, в него приходится внедрять TimeProvider.
// В Go тот же кеш вызывает time.Now() напрямую — synctest подменяет сами часы.
public class TtlCache(TimeSpan ttl, TimeProvider time)
{
    private readonly Dictionary<string, (string Value, DateTimeOffset Expires)> _items = new();
    private readonly object _lock = new();

    public void Set(string key, string value)
    {
        lock (_lock) _items[key] = (value, time.GetUtcNow() + ttl);
    }

    public bool TryGet(string key, out string? value)
    {
        lock (_lock)
        {
            if (_items.TryGetValue(key, out var e) && time.GetUtcNow() < e.Expires)
            {
                value = e.Value;
                return true;
            }
            value = null;
            return false;
        }
    }
}

public class ManualTimeProvider(DateTimeOffset start) : TimeProvider
{
    private DateTimeOffset _now = start;
    public override DateTimeOffset GetUtcNow() => _now;
    public void Advance(TimeSpan by) => _now += by;
}

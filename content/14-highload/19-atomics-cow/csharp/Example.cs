using System;
using System.Collections.Immutable;
using System.Threading;
using System.Threading.Tasks;

// CAS-цикл на Interlocked.CompareExchange — как CompareAndSwap в Go.
long used = 0;
const long Limit = 5000;
int granted = 0;
Parallel.For(0, 8000, _ =>
{
    while (true)
    {
        long cur = Volatile.Read(ref used);
        if (cur >= Limit) return;
        if (Interlocked.CompareExchange(ref used, cur + 1, cur) == cur)
        {
            Interlocked.Increment(ref granted);
            return;
        }
    }
});
Console.WriteLine(granted); // 5000

// Copy-on-write: неизменяемый снимок и атомарная замена ссылки.
var holder = new ConfigHolder(new Config(0, ImmutableDictionary<string, int>.Empty));
Parallel.For(0, 100, _ => holder.Update(old => old with
{
    Version = old.Version + 1,
    Limits = old.Limits.SetItem("api", old.Version + 1),
}));
Console.WriteLine($"{holder.Current.Version} {holder.Current.Limits["api"]}"); // 100 100

// ImmutableInterlocked делает тот же цикл за вас.
var flags = ImmutableDictionary<string, bool>.Empty;
ImmutableInterlocked.AddOrUpdate(ref flags, "beta", true, (_, _) => true);
Console.WriteLine(flags["beta"]);

sealed record Config(int Version, ImmutableDictionary<string, int> Limits);

sealed class ConfigHolder(Config initial)
{
    private Config _current = initial;

    // Volatile.Read — чтение с семантикой acquire; atomic.Pointer.Load
    // в Go к тому же последовательно согласован.
    public Config Current => Volatile.Read(ref _current);

    public void Update(Func<Config, Config> f)
    {
        while (true)
        {
            var old = Current;
            var next = f(old);
            if (Interlocked.CompareExchange(ref _current, next, old) == old) return;
        }
    }
}

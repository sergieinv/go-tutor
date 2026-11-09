// Публикация через канал — в C# обычно Task: await видит всё,
// что записала задача до завершения.
string msg = "";
await Task.Run(() => msg = "привет из задачи");
Console.WriteLine($"Task: {msg}");

// Семафор вместо буферизованного канала
var sem = new SemaphoreSlim(2);
int inside = 0, peak = 0;
await Task.WhenAll(Enumerable.Range(0, 8).Select(_ => Task.Run(async () =>
{
    await sem.WaitAsync();
    int n = Interlocked.Increment(ref inside);
    int p;
    while (n > (p = Volatile.Read(ref peak)) && Interlocked.CompareExchange(ref peak, n, p) != p) { }
    Interlocked.Decrement(ref inside);
    sem.Release();
})));
Console.WriteLine($"семафор: не больше 2 — {peak <= 2}");

// atomic.Pointer[Config] — Volatile.Write/Read ссылки (release/acquire)
Config? current = null;
var writer = Task.Run(() => Volatile.Write(ref current, new Config("prod", 100)));
await writer;
if (Volatile.Read(ref current) is { } c)
    Console.WriteLine($"Volatile: {c.Name} {c.Limit}");

// sync.Once / sync.OnceValue — Lazy<T>
int calls = 0;
var lazy = new Lazy<Config>(() => { Interlocked.Increment(ref calls); return new Config("lazy", 7); });
var names = await Task.WhenAll(Enumerable.Range(0, 10).Select(_ => Task.Run(() => lazy.Value.Name)));
Console.WriteLine($"Lazy: вызовов {calls} видели {names[0]} {names[9]}");

// Двойная проверка: по ECMA-335 полю нужен volatile. Модель памяти
// CoreCLR и так делает запись ссылки release, но это гарантия
// конкретного рантайма; в Go аналогичной нет — быстрая проверка
// обязана быть атомарной.
Console.WriteLine($"Holder: {Holder.Get().Name}");

record Config(string Name, int Limit);

static class Holder
{
    private static volatile Config? _cfg; // по ECMA-335 без volatile — та же ошибка, что в Go
    private static readonly object _lock = new();

    public static Config Get()
    {
        if (_cfg is { } c) return c;
        lock (_lock)
        {
            return _cfg ??= new Config("dcl", 1);
        }
    }
}

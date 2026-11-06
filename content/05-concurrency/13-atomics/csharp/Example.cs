long requests = 0;
Parallel.For(0, 5000, _ => Interlocked.Increment(ref requests)); // requests.Add(1)
Console.WriteLine($"запросов: {Interlocked.Read(ref requests)}");

// CompareExchange возвращает прежнее значение, а не bool
int closed = 0;
var winners = 0;
Parallel.For(0, 10, _ =>
{
    if (Interlocked.CompareExchange(ref closed, 1, 0) == 0) // closed.CompareAndSwap(false, true)
        Interlocked.Increment(ref winners);
});
Console.WriteLine($"закрыто: {closed == 1} закрывших: {winners}");

// CAS-цикл для максимума
long maxSeen = 0;
Parallel.ForEach(new long[] { 17, 42, 8, 99, 23 }, v =>
{
    long cur;
    do
    {
        cur = Volatile.Read(ref maxSeen);
        if (v <= cur) return;
    } while (Interlocked.CompareExchange(ref maxSeen, v, cur) != cur);
});
Console.WriteLine($"максимум: {maxSeen}");

// Атомарная подмена неизменяемой конфигурации — atomic.Pointer[Config]
var cfg = new Config(1, 10);
var old = Interlocked.Exchange(ref cfg, new Config(2, 20));
Console.WriteLine($"было: {old} стало: {cfg}");

record Config(int Version, int Limit);

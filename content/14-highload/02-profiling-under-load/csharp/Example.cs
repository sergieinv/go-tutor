// Тот же сервис с горячей блокировкой. В .NET конкуренцию видно
// по счётчику monitor-lock-contention-count (dotnet-counters; в метре
// System.Runtime .NET 9+ — dotnet.monitor.lock_contentions) и событиям
// contention в трассе (dotnet-trace), а не в CPU-профиле.
var svc = new OrderService();
await Parallel.ForEachAsync(Enumerable.Range(0, 800), async (i, ct) =>
{
    svc.Handle(i, fixedVersion: false);
    await Task.Yield();
});
Console.WriteLine($"до исправления: {svc.Served} запросов");
Console.WriteLine($"конкуренция за lock была: {Monitor.LockContentionCount > 0}");

svc.Reset();
await Parallel.ForEachAsync(Enumerable.Range(0, 800), async (i, ct) =>
{
    svc.Handle(i, fixedVersion: true);
    await Task.Yield();
});
Console.WriteLine($"после исправления: {svc.Served} запросов");

sealed class OrderService
{
    private readonly object _gate = new();
    private int _served;
    private ulong _checksum;

    public int Served => Volatile.Read(ref _served);

    public void Reset() { lock (_gate) { _served = 0; } }

    public void Handle(int id, bool fixedVersion)
    {
        if (fixedVersion)
        {
            ulong sum = Work(id);          // тяжёлое — вне блокировки
            lock (_gate) { _served++; _checksum ^= sum; }
        }
        else
        {
            lock (_gate)                   // узкое место: работа под lock
            {
                _checksum ^= Work(id);
                _served++;
            }
        }
    }

    private static ulong Work(int id)
    {
        ulong h = 14695981039346656037UL ^ (ulong)id;
        for (int i = 0; i < 600_000; i++)
        {
            h ^= h >> 29;
            h *= 1099511628211UL;
        }
        return h;
    }
}

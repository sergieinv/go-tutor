var stats = new Stats();
var accounts = new Accounts { A = 1000 };
await Task.WhenAll(Enumerable.Range(0, 100).Select(_ => Task.Run(() =>
{
    stats.Inc("get");
    accounts.Move(1);
})));
Console.WriteLine($"get: {stats.Get("get")}");
Console.WriteLine($"сумма на счетах: {accounts.Total()}");

class Stats
{
    // Отдельный объект-замок; lock реентерабелен (Monitor)
    private readonly object _gate = new();
    private readonly Dictionary<string, int> _counts = new();

    public void Inc(string key)
    {
        lock (_gate)
        {
            _counts[key] = Get(key) + 1; // повторный вход в lock — можно; в Go — дедлок
        }
    }

    public int Get(string key)
    {
        lock (_gate) return _counts.GetValueOrDefault(key);
    }
}

class Accounts
{
    private readonly object _gate = new();
    public int A, B;

    // Оба поля под одним замком — инвариант A + B не нарушается
    public void Move(int n) { lock (_gate) { A -= n; B += n; } }
    public int Total() { lock (_gate) return A + B; }
}

// Аналог sync.RWMutex — ReaderWriterLockSlim:
// rw.EnterReadLock() / ExitReadLock(), rw.EnterWriteLock() / ExitWriteLock()

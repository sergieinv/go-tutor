using System.Buffers;
using System.Collections.Concurrent;
using System.Text;

// ArrayPool<T>.Shared — пул массивов из BCL. Rent(n) даёт массив
// длиной НЕ МЕНЬШЕ n, Return по умолчанию не очищает его.
byte[] buf = ArrayPool<byte>.Shared.Rent(100);
Console.WriteLine($"Rent(100): длина {buf.Length} >= 100 — {buf.Length >= 100}");
try
{
    int n = Encoding.UTF8.GetBytes("user-42: 1,22,333", buf);
    Console.WriteLine(Encoding.UTF8.GetString(buf, 0, n));
}
finally
{
    // clearArray: true — аналог Reset; по умолчанию false
    ArrayPool<byte>.Shared.Return(buf, clearArray: true);
}

// StringBuilder из пула — как *bytes.Buffer в sync.Pool.
// ObjectPool<T> из Microsoft.Extensions.ObjectPool устроен похоже,
// но держит объекты сильными ссылками: GC их не забирает.
var pool = new SimplePool<StringBuilder>(() => new StringBuilder(64), maxRetained: 16);
string Format(string user, int[] items)
{
    var sb = pool.Rent();
    try
    {
        sb.Clear();                              // buf.Reset()
        sb.Append(user).Append(": ").AppendJoin(',', items);
        return sb.ToString();
    }
    finally
    {
        if (sb.Capacity <= 64 * 1024) pool.Return(sb); // огромные не возвращаем
    }
}
Console.WriteLine(Format("ann", [3, 14, 159]));
Console.WriteLine(Format("bob", []));

// Минимальный пул на ConcurrentBag — по поведению ближе к free-list
// на канале, чем к sync.Pool: объекты живут, пока их не взяли.
sealed class SimplePool<T>(Func<T> create, int maxRetained) where T : class
{
    private readonly ConcurrentBag<T> _items = new();

    public T Rent() => _items.TryTake(out var x) ? x : create();

    public void Return(T x)
    {
        if (_items.Count < maxRetained) _items.Add(x);
    }
}

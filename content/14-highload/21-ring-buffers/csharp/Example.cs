using System;
using System.Collections.Concurrent;
using System.Threading;
using System.Threading.Channels;
using System.Threading.Tasks;

// Обычный выбор в .NET — Channel<T>: ограниченный буфер с ожиданием.
// SingleReader/SingleWriter — подсказки для оптимизации, но CreateBounded
// в .NET 10 их игнорирует; учитывает их только CreateUnbounded
// (SingleReader = true даёт отдельную реализацию). В Go канал один на все случаи.
var channel = Channel.CreateBounded<int>(new BoundedChannelOptions(1024)
{
    SingleReader = true,
    SingleWriter = true,
    FullMode = BoundedChannelFullMode.Wait,
});
var producer = Task.Run(async () =>
{
    for (int i = 0; i < 1000; i++) await channel.Writer.WriteAsync(i);
    channel.Writer.Complete();
});
long sum = 0;
await foreach (var v in channel.Reader.ReadAllAsync()) sum += v;
await producer;
Console.WriteLine(sum); // 499500

// ConcurrentQueue<T> — MPMC без блокировок, но без ограничения длины.
var queue = new ConcurrentQueue<int>();
queue.Enqueue(1);
Console.WriteLine(queue.TryDequeue(out var x) ? x : -1);

// SPSC-кольцо руками: Volatile.Write публикует tail после записи ячейки,
// Volatile.Read у потребителя видит её (release/acquire).
var ring = new SpscRing<int>(8);
Console.WriteLine($"{ring.TryPush(42)} {ring.TryPop(out var y)} {y}");

sealed class SpscRing<T>(int sizePowerOfTwo)
{
    private readonly T[] _buf = new T[sizePowerOfTwo];
    private long _head; // пишет только потребитель
    private long _tail; // пишет только производитель (в реальном коде — в своей кеш-линии)

    public bool TryPush(T value)
    {
        long t = _tail;
        if (t - Volatile.Read(ref _head) == _buf.Length) return false;
        _buf[t & (_buf.Length - 1)] = value;
        Volatile.Write(ref _tail, t + 1);
        return true;
    }

    public bool TryPop(out T value)
    {
        long h = _head;
        if (h == Volatile.Read(ref _tail)) { value = default!; return false; }
        int i = (int)(h & (_buf.Length - 1));
        value = _buf[i];
        _buf[i] = default!; // не держать ссылку для GC
        Volatile.Write(ref _head, h + 1);
        return true;
    }
}

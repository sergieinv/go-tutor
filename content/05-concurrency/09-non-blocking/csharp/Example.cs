using System.Threading.Channels;

// TryRead / TryWrite — неблокирующие операции канала
var ch = Channel.CreateBounded<string>(1);
Console.WriteLine($"есть сообщение: {ch.Reader.TryRead(out _)}");
ch.Writer.TryWrite("привет");
if (ch.Reader.TryRead(out var msg)) Console.WriteLine($"есть сообщение: True {msg}");

// «Отбросить, если занято» — политика заполнения канала
// При DropWrite TryWrite всегда возвращает true; о потерях сообщает
// колбэк itemDropped (.NET 6+)
var dropped = 0;
var queue = Channel.CreateBounded<string>(new BoundedChannelOptions(3)
{
    FullMode = BoundedChannelFullMode.DropWrite,
}, itemDropped: _ => dropped++);
for (var i = 0; i < 5; i++)
{
    queue.Writer.TryWrite($"событие {i}");
}
Console.WriteLine($"в очереди: {queue.Reader.Count} потеряно: {dropped}");

// Try-lock: попытка захвата без ожидания
var sem = new SemaphoreSlim(1, 1);
Console.WriteLine($"первая попытка: {sem.Wait(0)}");
Console.WriteLine($"вторая попытка: {sem.Wait(0)}");
sem.Release();
Console.WriteLine($"после освобождения: {sem.Wait(0)}");

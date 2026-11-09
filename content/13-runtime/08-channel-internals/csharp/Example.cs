using System.Threading.Channels;

// Ограниченный канал. FullMode = Wait — как в Go: писатель ждёт места
// (асинхронно, без блокировки потока).
var ch = Channel.CreateBounded<int>(new BoundedChannelOptions(3)
{
    FullMode = BoundedChannelFullMode.Wait,
});
await ch.Writer.WriteAsync(10);
await ch.Writer.WriteAsync(20);
Console.WriteLine($"Count: {ch.Reader.Count}"); // аналог len(ch), такой же мгновенный снимок
await ch.Reader.ReadAsync();
await ch.Reader.ReadAsync();

// Ждущие читатели — незавершённые ValueTask в очереди канала.
// Писатель завершает первого из них напрямую, в порядке прихода.
var readers = Enumerable.Range(0, 3)
    .Select(async i => $"r{i}←{await ch.Reader.ReadAsync()}")
    .ToArray();
for (var v = 0; v < 3; v++) await ch.Writer.WriteAsync(v);
Console.WriteLine(string.Join(" ", await Task.WhenAll(readers)));

// Ёмкость 0 — рандеву, как небуферизованный канал Go:
// без ждущего читателя TryWrite не проходит, WriteAsync ждёт читателя.
var rendezvous = Channel.CreateBounded<int>(0);
Console.WriteLine($"TryWrite без читателя: {rendezvous.Writer.TryWrite(1)}");

// Неблокирующая попытка — TryWrite вместо select с default.
var full = Channel.CreateBounded<int>(1);
Console.WriteLine($"{full.Writer.TryWrite(1)} {full.Writer.TryWrite(2)}"); // True False

// Complete — аналог close: буфер дочитывается, потом чтение заканчивается.
full.Writer.Complete();
await foreach (var v in full.Reader.ReadAllAsync()) Console.WriteLine($"дочитали {v}");

// Запись в завершённый канал — false или исключение, а не паника процесса.
Console.WriteLine($"TryWrite после Complete: {full.Writer.TryWrite(3)}");
try
{
    await full.Writer.WriteAsync(4);
}
catch (ChannelClosedException)
{
    Console.WriteLine("ChannelClosedException");
}

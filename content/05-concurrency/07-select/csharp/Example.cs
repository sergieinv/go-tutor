using System.Threading.Channels;

// Ожидание первого из двух источников
var fast = Task.Delay(50).ContinueWith(_ => "быстрый");
var slow = Task.Delay(150).ContinueWith(_ => "медленный");
var first = await Task.WhenAny(fast, slow);
Console.WriteLine($"получено: {await first}");

// Таймаут — гонка с Task.Delay
var never = new TaskCompletionSource<string>().Task;
var winner = await Task.WhenAny(never, Task.Delay(100));
if (winner != never) Console.WriteLine("таймаут");

// Неблокирующее чтение — TryRead вместо select с default
var ch = Channel.CreateBounded<int>(1);
if (!ch.Reader.TryRead(out var value)) Console.WriteLine("никто не готов");

// Неблокирующая отправка — TryWrite
for (var i = 0; i < 2; i++)
{
    if (ch.Writer.TryWrite(i)) Console.WriteLine($"отправлено: {i}");
    else Console.WriteLine($"буфер полон, пропускаем: {i}");
}

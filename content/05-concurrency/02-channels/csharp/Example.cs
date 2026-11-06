using System.Threading.Channels;

// Ближайший аналог небуферизованного канала — Bounded с ёмкостью 1:
// ёмкости 0 (точки встречи) в System.Threading.Channels нет.
var ch = Channel.CreateBounded<string>(1);

// Отправитель — отдельная задача, как горутина в Go.
_ = Task.Run(async () => await ch.Writer.WriteAsync("привет"));

// ReadAsync не блокирует поток: остаток метода станет продолжением.
var msg = await ch.Reader.ReadAsync();
Console.WriteLine($"получено: {msg}");

// Вернуть результат из фоновой работы — через Task<T>, а не канал.
var sum = await Task.Run(() => Enumerable.Range(1, 100).Sum());
Console.WriteLine($"сумма 1..100: {sum}");

// Значения приходят в порядке отправки (FIFO).
var words = Channel.CreateBounded<string>(1);
_ = Task.Run(async () =>
{
    foreach (var w in new[] { "раз", "два", "три" })
        await words.Writer.WriteAsync(w.ToUpper());
});
for (var i = 0; i < 3; i++)
    Console.WriteLine(await words.Reader.ReadAsync());

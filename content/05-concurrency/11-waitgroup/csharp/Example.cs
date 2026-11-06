var words = new[] { "go", "channel", "select", "mutex" };

// Task.WhenAll собирает и ждёт, и результаты — в порядке задач
var upper = await Task.WhenAll(words.Select(w => Task.Run(() => w.ToUpperInvariant())));
Console.WriteLine(string.Join(" ", upper));

// Ближайший аналог WaitGroup — CountdownEvent: только счётчик
var lengths = new int[words.Length];
using var countdown = new CountdownEvent(words.Length); // как wg.Add(n)
for (var i = 0; i < words.Length; i++)
{
    var idx = i; // переменная цикла for в C# общая на все итерации — копируем
    ThreadPool.QueueUserWorkItem(_ =>
    {
        try { lengths[idx] = words[idx].Length; }
        finally { countdown.Signal(); } // как defer wg.Done()
    });
}
countdown.Wait(); // как wg.Wait()
Console.WriteLine(string.Join(" ", lengths));

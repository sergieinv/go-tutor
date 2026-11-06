// yield return: компилятор строит конечный автомат, потребитель тянет MoveNext()
foreach (var v in Countdown(10))
{
    if (v == 8) break; // вызовет Dispose → выполнится finally в итераторе
    Console.WriteLine(v);
}

// LINQ — готовые адаптеры; в Go их пишут сами поверх iter.Seq
var upper = new[] { "go", "c#" }.Select(s => s.ToUpper()).ToList();
Console.WriteLine(string.Join(",", upper));

// Ручной pull — аналог iter.Pull
using var e = Countdown(2).GetEnumerator();
e.MoveNext();
Console.WriteLine(e.Current); // 2

static IEnumerable<int> Countdown(int from)
{
    try
    {
        for (var i = from; i >= 0; i--)
            yield return i;
    }
    finally
    {
        Console.WriteLine("  итератор закрыт");
    }
}

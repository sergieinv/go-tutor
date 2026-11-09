using System;
using System.Collections.Generic;

// В C# место значения задаёт тип, а не компилятор:
// class — куча, struct — там, где объявлен.
var onStack = new PointS(1, 2);     // struct: в кадре метода, без кучи
var onHeap = new PointC(1, 2);      // class: объект в куче (если JIT не докажет обратное)
Console.WriteLine(onStack.X + onHeap.X);

// Стековый буфер просят явно. Span<T> — ref struct,
// компилятор не даст ему пережить метод.
Span<byte> buf = stackalloc byte[64];
Console.WriteLine(Sum(buf));

// Упаковка (boxing): int копируется в объект в куче —
// как значение, упакованное в any в Go.
object boxed = 1000;
Console.WriteLine(boxed);

// Лямбда, захватившая локальную переменную, переносит её
// в объект-замыкание в куче — даже если вызывается на месте.
int n = 0;
Action inc = () => n++;
inc();
Console.WriteLine(n);

// Без упаковки: форматирование числа прямо в стековый буфер —
// аналог strconv.AppendInt в Go.
Span<char> text = stackalloc char[20];
if (12345.TryFormat(text, out int written))
    Console.WriteLine(text[..written].ToString());

// Дженерик-сортировка без интерфейса на каждый вызов.
var words = new List<string> { "ccc", "a", "bb" };
words.Sort(static (a, b) => a.Length != b.Length ? a.Length.CompareTo(b.Length) : string.CompareOrdinal(a, b));
Console.WriteLine(string.Join(",", words));

static int Sum(Span<byte> s)
{
    int total = 0;
    foreach (var b in s) total += b;
    return total;
}

readonly record struct PointS(int X, int Y);
sealed record PointC(int X, int Y);

// В .NET тип и таблица методов — в заголовке объекта, ссылка на интерфейс —
// обычная ссылка (8 байт). В Go тип/itab хранится в самом интерфейсе (16 байт).
IShape s = new Square(4);
Console.WriteLine(s.Area()); // вызов через virtual stub dispatch; Dynamic PGO может девиртуализировать

// Boxing: значимый тип копируется в объект в куче — как упаковка в any.
var sq = new Square(3);
object boxed = sq;
sq = sq with { Side = 10 };
Console.WriteLine($"{((Square)boxed).Area()} {sq.Area()}"); // 9 100

// В Go маленькие числа упаковываются без аллокации (staticuint64s),
// в C# каждый box — объект (если JIT не уберёт его escape-анализом).
object a = 7, b = 7;
Console.WriteLine(ReferenceEquals(a, b)); // False: два разных объекта

// Сравнение object в C# не бросает исключений...
object x = new[] { 1 }, y = new[] { 1 };
Console.WriteLine(x.Equals(y)); // False: сравнение ссылок
// ...а в Go any([]int{1}) == any([]int{1}) — паника во время выполнения.

// Проверка типа — аналог type assertion и type switch.
object o = s;
if (o is Square q) Console.WriteLine(q.Side);
var kind = o switch
{
    Square => "square",
    IShape => "shape",
    _ => "other",
};
Console.WriteLine(kind);

interface IShape { int Area(); }

readonly record struct Square(int Side) : IShape
{
    public int Area() => Side * Side;
}

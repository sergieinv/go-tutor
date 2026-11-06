using System.Numerics;

// Арифметика в generics — через INumber<T> и static abstract-члены (C# 11)
Console.WriteLine(Sum([1, 2, 3]));
Console.WriteLine(Sum([0.5, 0.25]));

// Сравнение — через IComparable<T> (оператор < доступен только
// с ограничением IComparisonOperators<T, T, bool>)
Console.WriteLine(Max("go", "c#"));

// Сортировка по нескольким ключам: ThenBy вместо cmp.Or
var staff = new[] { ("Bob", "ops", 41), ("Ann", "dev", 35), ("Eve", "dev", 29) };
foreach (var e in staff.OrderBy(e => e.Item2).ThenBy(e => e.Item3))
    Console.WriteLine(e);

static T Sum<T>(T[] items) where T : INumber<T>
{
    var total = T.Zero;
    foreach (var v in items) total += v;
    return total;
}

static T Max<T>(T a, T b) where T : IComparable<T> => a.CompareTo(b) > 0 ? a : b;

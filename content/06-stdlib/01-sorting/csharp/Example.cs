using System;
using System.Collections.Generic;
using System.Linq;

var users = new List<User>
{
    new("Олег", "ops", 41), new("Анна", "dev", 29),
    new("Иван", "dev", 25), new("Вера", "ops", 29),
};

// LINQ ленив и создаёт новую последовательность — users не меняется.
// OrderBy/ThenBy устойчивы.
var sorted = users
    .OrderBy(u => u.Dept)
    .ThenBy(u => u.Age)
    .ThenBy(u => u.Name, StringComparer.Ordinal)
    .ToList();

// List<T>.Sort — на месте и неустойчиво, как slices.SortFunc в Go.
// Несколько ключей — цепочка сравнений, как cmp.Or.
users.Sort((a, b) =>
{
    var c = string.CompareOrdinal(a.Dept, b.Dept);
    if (c != 0) return c;
    c = a.Age.CompareTo(b.Age);
    return c != 0 ? c : string.CompareOrdinal(a.Name, b.Name);
});

// BinarySearch: если не нашли — отрицательное число, ~index = позиция вставки.
var nums = new List<int> { 1, 2, 5, 8 };
int i = nums.BinarySearch(6);
Console.WriteLine(i < 0 ? $"вставить в {~i}" : $"нашли в {i}");

record User(string Name, string Dept, int Age);

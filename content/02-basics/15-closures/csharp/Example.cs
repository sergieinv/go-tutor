// Лямбда захватывает переменную, а не значение — как в Go
static Func<int> Counter()
{
    var n = 0;
    return () => ++n;
}

var c = Counter();
Console.WriteLine($"{c()} {c()} {c()}"); // 1 2 3

var x = 1;
Func<int> show = () => x;
x = 2;
Console.WriteLine(show()); // 2

// foreach: с C# 5 своя переменная на каждой итерации
var fromForeach = new List<Func<int>>();
foreach (var i in new[] { 0, 1, 2 }) fromForeach.Add(() => i);
Console.WriteLine(string.Join(" ", fromForeach.Select(f => f()))); // 0 1 2

// for: переменная одна на весь цикл и в современном C#.
// В Go с 1.22 и for, и range дают 0 1 2.
var fromFor = new List<Func<int>>();
for (var i = 0; i < 3; i++) fromFor.Add(() => i);
Console.WriteLine(string.Join(" ", fromFor.Select(f => f()))); // 3 3 3

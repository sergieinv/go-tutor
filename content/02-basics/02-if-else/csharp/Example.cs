int t = 23;
if (t < 0) Console.WriteLine("мороз");   // скобки вокруг условия, тело без { }
else if (t < 20) Console.WriteLine("прохладно");
else Console.WriteLine("тепло");

// out var «утекает» во внешнюю область: n видна и после if
if (!int.TryParse("42", out var n))
    return;
Console.WriteLine(n * 2);

// Тернарный оператор — в Go его нет
int a = 3, b = 7;
int larger = a > b ? a : b;
Console.WriteLine(Math.Max(a, b) == larger);

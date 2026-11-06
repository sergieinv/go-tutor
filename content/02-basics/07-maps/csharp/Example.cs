var ages = new Dictionary<string, int> { ["ann"] = 31, ["bob"] = 27 };

// Отсутствующий ключ — исключение, а не нулевое значение
try { _ = ages["eve"]; }
catch (KeyNotFoundException) { Console.WriteLine("нет eve"); }

// Comma ok из Go — это TryGetValue
if (ages.TryGetValue("ann", out var age)) Console.WriteLine(age);

// Счётчик: без нулевого значения нужен GetValueOrDefault
var counts = new Dictionary<string, int>();
foreach (var w in "to be or not to be".Split(' '))
    counts[w] = counts.GetValueOrDefault(w) + 1;

// Стабильный порядок — сортировка ключей
foreach (var k in counts.Keys.Order()) Console.Write($"{k}={counts[k]} ");

ages.Remove("bob");   // delete(ages, "bob")
ages.Clear();         // clear(ages)

Dictionary<string, int>? none = null;
// none["x"] — NullReferenceException; nil-map в Go читать можно

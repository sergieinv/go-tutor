var langs = new List<string> { "go", "c#", "rust" };

// Индекс в foreach не приходит — нужен for или Select((x, i) => ...)
foreach (var (l, i) in langs.Select((l, i) => (l, i)))
    Console.Write($"{i}={l} ");

// Изменение коллекции внутри foreach — исключение
try
{
    foreach (var l in langs) langs.Add(l);
}
catch (InvalidOperationException) { Console.WriteLine("коллекция изменена"); }

// foreach по string даёт char — UTF-16, а не руны
foreach (char c in "go, мир") Console.Write(c);

// Для класса foreach меняет сам объект (ссылка),
// для struct изменить переменную итерации нельзя совсем.
var items = new List<Item> { new("pen", 1) };
foreach (var it in items) it.Qty = 100;

class Item(string name, int qty)
{
    public string Name = name;
    public int Qty = qty;
}

using System.Text;

var s = "Go, мир";

// Length считает кодовые единицы UTF-16: 7
Console.WriteLine(s.Length);

// В UTF-8 эта строка занимает 10 байтов — столько вернёт len(s) в Go
Console.WriteLine(Encoding.UTF8.GetByteCount(s));

// s[4] — char 'м', а в Go s[4] — байт 208
Console.WriteLine(s[4]);

// Эмодзи не помещается в один char: Length == 2
var smile = "😀";
Console.WriteLine(smile.Length);
// Rune (.NET Core 3.0+) — аналог rune в Go
foreach (Rune r in smile.EnumerateRunes())
    Console.WriteLine($"U+{r.Value:X4}");

// GetBytes копирует, как []byte(s) в Go
byte[] bytes = Encoding.UTF8.GetBytes(s);
bytes[0] = (byte)'g';
Console.WriteLine(Encoding.UTF8.GetString(bytes)); // go, мир
Console.WriteLine(s);                              // Go, мир

// Подстрока в C# копирует; s[1:4] в Go — нет
Console.WriteLine(s.Substring(1, 3));

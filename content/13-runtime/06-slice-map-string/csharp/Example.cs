using System.Runtime.InteropServices;
using System.Text;

// List<T> растёт ровно вдвое: 4, 8, 16… — в Go после 256 элементов ×1,25
// и округление до классов размера аллокатора.
var list = new List<int>();
var caps = new List<int>();
for (var i = 0; i < 20; i++)
{
    list.Add(i);
    if (caps.Count == 0 || caps[^1] != list.Capacity) caps.Add(list.Capacity);
}
Console.WriteLine(string.Join(" ", caps)); // 4 8 16 32

// string — объект в куче (UTF-16). Substring копирует символы,
// окно без копии — ReadOnlySpan<char>, как подстрока s[i:j] в Go.
var text = "hello, gopher";
string copy = text.Substring(7);
ReadOnlySpan<char> view = text.AsSpan(7);
Console.WriteLine($"{copy} {view.ToString()}");

// Байты ↔ строка — всегда копия (и перекодирование UTF-16 ↔ UTF-8).
byte[] bytes = Encoding.UTF8.GetBytes(text);
string back = Encoding.UTF8.GetString(bytes);
Console.WriteLine(back == text);

// Обход копии — через MemoryMarshal, как unsafe.Slice/unsafe.String в Go:
// span смотрит в память строки, менять её нельзя.
ReadOnlySpan<byte> raw = MemoryMarshal.AsBytes(text.AsSpan());
Console.WriteLine(raw.Length); // 26: два байта на символ UTF-16

// Dictionary: Clear не уменьшает внутренние массивы, TrimExcess — уменьшает.
// В Go аналога TrimExcess нет: только новая map.
var dict = new Dictionary<int, int>();
for (var i = 0; i < 100_000; i++) dict[i] = i;
dict.Clear();
dict.TrimExcess();
Console.WriteLine(dict.Count);

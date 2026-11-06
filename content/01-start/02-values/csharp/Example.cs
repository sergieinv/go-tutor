int n = 42;
long big = 1L << 40;

// Неявное расширение: int → long и int → double
long sum = n + big;
double d = n;

// Явное приведение — синтаксис (T)x; усечение к нулю, как в Go
int t = (int)3.99; // 3

// Переполнение по умолчанию тоже заворачивается...
byte b = 255;
b++; // 0
// ...но можно включить проверку — в Go такого режима нет
// checked { b++; } // OverflowException

// Строки и числа — Parse/ToString вместо strconv
int parsed = int.Parse("42");
string s = parsed.ToString() + "!";

// Форматирование — интерполяция и спецификаторы формата
Console.WriteLine($"[{n,5}] [{3.14159:F2}] [{255:x}] [{Convert.ToString(5, 2).PadLeft(8, '0')}]");
Console.WriteLine($"{n.GetType()} {d.GetType()}"); // System.Int32 System.Double — аналог %T

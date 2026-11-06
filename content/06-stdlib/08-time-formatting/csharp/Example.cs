using System;
using System.Globalization;

var t = new DateTimeOffset(2024, 3, 5, 15, 4, 9, 123, TimeSpan.FromHours(3));
var inv = CultureInfo.InvariantCulture;

// Буквенные спецификаторы вместо эталонной даты
Console.WriteLine(t.ToString("yyyy-MM-ddTHH:mm:sszzz", inv)); // ≈ time.RFC3339
Console.WriteLine(t.ToString("dd.MM.yyyy HH:mm", inv));       // "02.01.2006 15:04"
Console.WriteLine(t.ToString("HH:mm:ss.fff zzz", inv));       // "15:04:05.000 -07:00"
Console.WriteLine(t.ToString("hh:mm tt", inv));               // "03:04 PM"

// ParseExact: без смещения в строке — DateTimeKind.Unspecified (в Go Parse даст UTC)
var p = DateTime.ParseExact("2024-03-05 10:00", "yyyy-MM-dd HH:mm", inv);
Console.WriteLine(p.Kind); // Unspecified

// Ошибка разбора — исключение (или TryParseExact), в Go — error вторым результатом
bool ok = DateTime.TryParseExact("2024-02-30", "yyyy-MM-dd", inv, DateTimeStyles.None, out _);
Console.WriteLine(ok); // False

// Unix-время
Console.WriteLine(t.ToUnixTimeSeconds());
Console.WriteLine(DateTimeOffset.FromUnixTimeMilliseconds(1_700_000_000_123));

using System;
using System.Diagnostics;

// DateTimeOffset ближе всего к time.Time: момент + смещение
var msk = TimeSpan.FromHours(3);
var t = new DateTimeOffset(2024, 3, 15, 18, 30, 0, msk);
Console.WriteLine($"{t} {t.DayOfWeek} {t.DayOfYear}");

// TimeSpan — тики по 100 нс; в Go Duration — наносекунды
var d = TimeSpan.FromMinutes(90) + TimeSpan.FromSeconds(30);
Console.WriteLine($"{d} {d.TotalHours}"); // 01:30:30 1.508…

var later = t.AddHours(36);
Console.WriteLine(later - t);              // 1.12:00:00
Console.WriteLine(new DateTime(2024, 1, 31).AddMonths(1)); // 29.02.2024 — в Go AddDate даст 2 марта!

// == у DateTimeOffset сравнивает моменты; у time.Time — нет, нужен Equal
var u = t.ToUniversalTime();
Console.WriteLine(t == u);                 // True
Console.WriteLine(t.EqualsExact(u));       // False — ближе к == в Go

// Отдельный Stopwatch для замеров; в Go монотонные часы встроены в time.Now()
var sw = Stopwatch.StartNew();
Console.WriteLine(sw.Elapsed >= TimeSpan.Zero);

// default(DateTime) = 01.01.0001 — как нулевой time.Time; «нет даты» — DateTime?
DateTime? deadline = null;
Console.WriteLine(deadline is null);

using System;
using System.Globalization;

// Отдельный метод на каждый тип; ошибка — исключение
try { byte.Parse("300"); }
catch (OverflowException) { Console.WriteLine("переполнение"); } // в Go: ErrRange и значение 127/255

try { int.Parse("12a"); }
catch (FormatException) { Console.WriteLine("не число"); }       // в Go: ErrSyntax

// Основание — только 2, 8, 10, 16
Console.WriteLine(Convert.ToInt32("ff", 16));     // 255
Console.WriteLine(Convert.ToString(255, 2));      // 11111111 — как strconv.FormatInt(255, 2)

// TryParse без исключений — ближе всего к (value, err) в Go, но без причины ошибки
Console.WriteLine(long.TryParse("9223372036854775808", out _)); // False

// double зависит от культуры; в Go ParseFloat понимает только точку
Console.WriteLine(double.Parse("1,5", new CultureInfo("ru-RU"))); // 1.5
Console.WriteLine(double.Parse("1.5", CultureInfo.InvariantCulture));

// bool.Parse принимает только "True"/"False" без учёта регистра; ParseBool — ещё 1/0/t/f
Console.WriteLine(bool.Parse("TRUE"));

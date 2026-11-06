using System;
using System.Globalization;

var p = new Point(1, 2);
// Интерполяция и форматные спецификаторы: {значение,ширина:формат}
Console.WriteLine($"[{3.14159,8:F3}] [{"left",-10}] [{42,6}]");
Console.WriteLine($"{255:x} {3054:X}");

// ToString() — аналог String() из fmt.Stringer
var t = new Celsius(21.55);
Console.WriteLine(t);          // 21.6°C
Console.WriteLine(p);          // record печатает Point { X = 1, Y = 2 } — почти %+v

// string.Format с позиционными аргументами — ближе всего к Sprintf
var s = string.Format(CultureInfo.InvariantCulture, "{0}%", 95);
Console.WriteLine($"загрузка: {s}");

// Неверный формат — исключение во время выполнения, а не пометка в выводе
try { string.Format("{1}", "x"); }
catch (FormatException e) { Console.WriteLine(e.GetType().Name); }

record Point(int X, int Y);

readonly record struct Celsius(double Value)
{
    public override string ToString() =>
        Value.ToString("F1", CultureInfo.InvariantCulture) + "°C";
}

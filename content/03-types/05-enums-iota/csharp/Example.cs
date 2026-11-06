Console.WriteLine(Weekday.Monday);         // Monday — ToString() из коробки
Console.WriteLine((int)Weekday.Wednesday); // 3
Console.WriteLine((Weekday)42);            // 42 — диапазон тоже не проверяется
Console.WriteLine(Enum.IsDefined((Weekday)42)); // False — в Go такой проверки нет

var s = Style.Bold | Style.Underline;
Console.WriteLine(s);                       // Bold, Underline
Console.WriteLine(s.HasFlag(Style.Italic)); // False
s &= ~Style.Bold;                           // в Go: s &^= Bold
Console.WriteLine(s);                       // Underline

enum Weekday { Sunday, Monday, Tuesday, Wednesday }

[Flags]
enum Style : byte
{
    Bold = 1 << 0,
    Italic = 1 << 1,
    Underline = 1 << 2,
}

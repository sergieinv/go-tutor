var list = new List<string> { "a", "b", "c" };
list.Add("d"); // меняет список на месте

// GetRange копирует элементы: изменение части не видно в list
var part = list.GetRange(1, 2);
part[0] = "B";
Console.WriteLine(string.Join(",", list)); // a,b,c,d

// Ближайший аналог среза — Span<T>: окно в массив без копирования
var arr = new[] { "a", "b", "c", "d" };
Span<string> span = arr.AsSpan(1, 2);
span[0] = "B";
Console.WriteLine(string.Join(",", arr)); // a,B,c,d

// Но Span<T> нельзя «дорастить» — у среза Go есть append
// и ёмкость, которая решает, будет ли новый массив.

// class — ссылочный тип: присваивание копирует ссылку
var a = new UserClass { Name = "Alice" };
var b = a;
b.Name = "Bob";
Console.WriteLine(a.Name); // Bob

// record struct — значимый тип с поэлементным ==, ближе всего к struct Go
var p1 = new Point(1, 2);
var p2 = p1 with { X = 10 }; // копия с изменённым полем
Console.WriteLine(p1 == new Point(1, 2)); // True
Console.WriteLine(p2);                    // Point { X = 10, Y = 2 }

// Анонимный тип: только для чтения, виден лишь внутри метода
var cfg = new { Host = "localhost", Port = 8080 };
Console.WriteLine($"{cfg.Host}:{cfg.Port}");

class UserClass { public string Name { get; set; } = ""; }
record struct Point(int X, int Y);

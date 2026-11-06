// Перегрузка и параметры по умолчанию — в Go их нет
static int Add(int a, int b) => a + b;
static int Add3(int a, int b, int c = 0) => a + b + c;
static string Greet(string name, int times = 1) =>
    string.Concat(Enumerable.Repeat($"hi {name} ", times));

Console.WriteLine(Add(2, 3));
Console.WriteLine(Add3(1, 2));            // c = 0 по умолчанию
Console.WriteLine(Greet(times: 2, name: "Go")); // именованные аргументы

// Func<...> — в Go это тип func(int, int) int
Func<int, int, int> f = Add;
Func<int, int, int> mul = (a, b) => a * b;
Console.WriteLine(Apply(f, 4, 5) + Apply(mul, 4, 5));

// Делегат — именованный тип функции; в Go: type op func(int, int) int
Op op = Add;
Console.WriteLine(op(1, 1));

static int Apply(Func<int, int, int> fn, int a, int b) => fn(a, b);

delegate int Op(int a, int b);

// Рекурсия выглядит так же
static int Fact(int n) => n <= 1 ? 1 : n * Fact(n - 1);
Console.WriteLine(Fact(5));

// Локальная функция может вызывать себя сразу.
// В Go литералу нужна заранее объявленная переменная:
// var count func(*tree) int; count = func(...) { ... count(...) }
static int Count(Tree? t) => t is null ? 0 : 1 + Count(t.Left) + Count(t.Right);

var root = new Tree("a", new Tree("b", null, null), new Tree("c", null, null));
Console.WriteLine(Count(root));

// Стек потока фиксирован (обычно 1 МБ). Глубокая рекурсия —
// StackOverflowException, который нельзя перехватить: процесс падает.
// В Go стек горутины растёт до 1 ГБ, а потом — fatal error: stack overflow.
static int Depth(int n) => n == 0 ? 0 : Depth(n - 1) + 1;
// Depth(10_000_000); // StackOverflowException

// Обходной путь в .NET — поток с большим стеком
var t = new Thread(() => Console.WriteLine(Depth(1_000_000)), maxStackSize: 256 * 1024 * 1024);
t.Start();
t.Join();

record Tree(string Val, Tree? Left, Tree? Right);

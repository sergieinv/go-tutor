using System.Runtime.CompilerServices;

// Миллион вложенных вызовов не помещается в стандартный стек потока
// (около 1 МБ): StackOverflowException завершит процесс, catch не поможет.
// Нужен поток с явно заданным стеком — размер фиксирован при создании.
var big = new Thread(() => Console.WriteLine($"глубина: {Depth(1_000_000)}"), maxStackSize: 256 << 20);
big.Start();
big.Join();

// Защита от переполнения — проверять запас стека заранее:
// исключение бросается, пока стек ещё есть, и его можно поймать.
try
{
    Guarded(100_000_000);
}
catch (InsufficientExecutionStackException)
{
    Console.WriteLine("стек на исходе — остановились сами");
}

// Ссылки на стек стережёт компилятор: Span над stackalloc — ref struct,
// его нельзя сохранить в поле класса или вернуть из метода.
// CLR стеки не двигает, а Go — двигает и исправляет указатели сам.
Span<byte> buf = stackalloc byte[256];
buf[0] = 42;
Console.WriteLine($"buf[0] = {buf[0]}");

static int Depth(int n) => n == 0 ? 0 : Depth(n - 1) + 1;

static int Guarded(int n)
{
    RuntimeHelpers.EnsureSufficientExecutionStack();
    return n == 0 ? 0 : Guarded(n - 1) + 1;
}

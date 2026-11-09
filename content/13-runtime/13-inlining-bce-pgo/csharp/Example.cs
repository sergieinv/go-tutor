using System.Buffers.Binary;
using System.Runtime.CompilerServices;

// Маленький метод JIT встроит сам; AggressiveInlining поднимает
// бюджет. В Go такой директивы нет — только //go:noinline.
Console.WriteLine($"{Add(2, 3)} {Scale(7)} {Twice(4)}");

// RyuJIT убирает проверки границ в цикле до arr.Length — как
// цикл до len(s) или range в Go.
int[] s = [1, 2, 3, 4, 5];
Console.WriteLine($"{Sum(s)} {SumFirst(s, 3)}");

// Чтение uint32 little-endian: BinaryPrimitives проверяет длину
// один раз — как подсказка _ = b[3] в Go.
byte[] b = [0x78, 0x56, 0x34, 0x12];
Console.WriteLine(BinaryPrimitives.ReadUInt32LittleEndian(b) == U32(b));

// Выход за границы — исключение, как паника в Go.
try { _ = s[5]; }
catch (IndexOutOfRangeException e) { Console.WriteLine(e.GetType().Name); }

// Dynamic PGO (.NET 8+, по умолчанию): tier 0 собирает статистику,
// tier 1 встраивает горячее и делает guarded devirtualization.
// В Go то же даёт default.pgo при сборке.

static int Add(int a, int b) => a + b;

[MethodImpl(MethodImplOptions.NoInlining)] // //go:noinline
static int AddNoInline(int a, int b) => a + b;

static int Scale(int x) => Add(x, x) * 3;

static int Twice(int x) => AddNoInline(x, x) + AddNoInline(x, 1);

static int Sum(int[] s)
{
    int t = 0;
    for (int i = 0; i < s.Length; i++) t += s[i]; // проверок нет
    return t;
}

static int SumFirst(int[] s, int n)
{
    // Span-срез проверяет границу один раз — как s = s[:n] в Go.
    ReadOnlySpan<int> span = s.AsSpan(0, n);
    int t = 0;
    foreach (int v in span) t += v;
    return t;
}

static uint U32(ReadOnlySpan<byte> b)
{
    _ = b[3]; // в C# такая подсказка тоже работает для Span
    return (uint)(b[0] | b[1] << 8 | b[2] << 16 | b[3] << 24);
}

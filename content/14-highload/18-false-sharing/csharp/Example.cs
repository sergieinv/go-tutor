using System;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Threading;
using System.Threading.Tasks;

// Плотно: соседние long лежат в одной кеш-линии.
var packed = new long[16];

// С выравниванием: каждый счётчик — в своей структуре размером 128 байт.
// В Go то же самое делает поле-заполнитель _ [120]byte.
var padded = new PaddedCounter[16];
Console.WriteLine($"PaddedCounter: {Unsafe.SizeOf<PaddedCounter>()} байт"); // 128

Parallel.For(0, 8, g =>
{
    for (int i = 0; i < 10_000; i++)
    {
        Interlocked.Increment(ref packed[g]);        // false sharing
        Interlocked.Increment(ref padded[g].Value);  // своя линия
    }
});

long sumPacked = 0, sumPadded = 0;
for (int i = 0; i < 16; i++)
{
    sumPacked += Volatile.Read(ref packed[i]);
    sumPadded += Volatile.Read(ref padded[i].Value);
}
Console.WriteLine($"{sumPacked} {sumPadded}"); // 80000 80000

// LayoutKind.Explicit + Size задают размер явно — аналог заполнителя.
[StructLayout(LayoutKind.Explicit, Size = 128)]
struct PaddedCounter
{
    [FieldOffset(0)] public long Value;
}

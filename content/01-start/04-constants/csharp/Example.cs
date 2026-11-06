// В C# константа всегда имеет тип
const int KiB = 1 << 10;
const int MiB = KiB << 10;

long a = KiB;    // неявное расширение int → long
double b = KiB;  // int → double
ushort c = KiB;  // неявно только потому, что константа помещается в ushort

// Константные выражения считаются в типе операндов:
// const long Huge = 1 << 100; // для int сдвиг берётся по модулю 32 — это 1 << 4
const long Huge = 1L << 62;    // больше 64 бит не получить

// Константы double считаются в double — с той же ошибкой округления
const double Sum = 0.1 + 0.2;
Console.WriteLine(Sum == 0.3); // False, а в Go для констант — true

// Как в Go: целые делятся нацело до присваивания
double half = 1 / 2; // 0

// Сложные «константы» — static readonly, в Go — переменная пакета
static class Defaults
{
    public static readonly string[] Names = { "a", "b" };
}

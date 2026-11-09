using System.Diagnostics;

// В .NET профиль снимают снаружи процесса:
//   dotnet-trace collect -p <pid>      — CPU-сэмплы через EventPipe
//   dotnet-counters monitor -p <pid>   — GC, аллокации, пул потоков
//   dotnet-gcdump collect -p <pid>     — снимок кучи
// В Go то же делает сама программа через runtime/pprof.

// Ближайший аналог pprof.Do с метками — Activity с тегами:
// контекст операции течёт через await, но попадает в трассы
// OpenTelemetry, а не в CPU-сэмплы.
using var source = new ActivitySource("Example");
using var listener = new ActivityListener
{
    ShouldListenTo = _ => true,
    Sample = (ref ActivityCreationOptions<ActivityContext> _) => ActivitySamplingResult.AllData,
};
ActivitySource.AddActivityListener(listener);

ulong sum;
using (var a = source.StartActivity("hash"))
{
    a?.SetTag("phase", "hash"); // pprof.Labels("phase", "hash")
    sum = HashRounds(2_500_000);
}

int median;
using (var a = source.StartActivity("sort"))
{
    a?.SetTag("phase", "sort");
    median = SortRounds(30);
}
Console.WriteLine($"хеш: {sum:x}, медиана: {median}");

// Аналог «сколько выделили» из heap-профиля — счётчик рантайма.
long before = GC.GetAllocatedBytesForCurrentThread();
var index = new Dictionary<int, byte[]>();
for (int i = 0; i < 200_000; i++) index[i] = new byte[32];
long allocated = GC.GetAllocatedBytesForCurrentThread() - before;
Console.WriteLine($"в индексе: {index.Count}, выделено больше 6 МБ: {allocated > 6_000_000}");

static ulong HashRounds(int n)
{
    ulong h = 14695981039346656037; // FNV-1a
    for (int i = 0; i < n; i++)
        for (int b = 0; b < 16; b++)
        {
            h ^= (ulong)(i >> b) & 0xff;
            h *= 1099511628211;
        }
    return h;
}

static int SortRounds(int rounds)
{
    uint x = 1;
    var s = new int[20_000];
    for (int r = 0; r < rounds; r++)
    {
        for (int i = 0; i < s.Length; i++)
        {
            x ^= x << 13; x ^= x >> 17; x ^= x << 5;
            s[i] = (int)(x % 1_000_000);
        }
        Array.Sort(s);
    }
    return s[s.Length / 2];
}

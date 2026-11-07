// BenchmarkDotNet — отдельный NuGet-пакет; запускается из консольного
// приложения в Release: BenchmarkRunner.Run<JoinBenchmarks>().
// Чтобы файл собирался без пакета, атрибуты объявлены заглушками ниже.
using System.Text;

var b = new JoinBenchmarks { Size = 3 };
b.Setup();
Console.WriteLine(b.Builder()); // item0,item1,item2

// [MemoryDiagnoser] ≈ -benchmem: колонка Allocated ≈ B/op.
[MemoryDiagnoser]
public class JoinBenchmarks
{
    // [Params] ≈ цикл по размерам с b.Run("n=100", ...)
    [Params(10, 100, 1000)]
    public int Size { get; set; }

    private string[] _parts = [];

    // [GlobalSetup] ≈ код до for b.Loop(): выполняется один раз.
    [GlobalSetup]
    public void Setup() =>
        _parts = Enumerable.Range(0, Size).Select(i => $"item{i}").ToArray();

    // Возвращаемое значение «потребляет» BenchmarkDotNet — так JIT
    // не выкинет вычисление. В Go то же самое делает b.Loop.
    [Benchmark(Baseline = true)]
    public string Naive()
    {
        var s = "";
        for (var i = 0; i < _parts.Length; i++)
            s += (i > 0 ? "," : "") + _parts[i];
        return s;
    }

    [Benchmark]
    public string Builder()
    {
        var sb = new StringBuilder(_parts.Sum(p => p.Length) + _parts.Length);
        for (var i = 0; i < _parts.Length; i++)
        {
            if (i > 0) sb.Append(',');
            sb.Append(_parts[i]);
        }
        return sb.ToString(); // в отличие от strings.Builder.String() — копия
    }
}

// Заглушки атрибутов BenchmarkDotNet, чтобы пример собирался без NuGet.
[AttributeUsage(AttributeTargets.Class)] class MemoryDiagnoserAttribute : Attribute { }
[AttributeUsage(AttributeTargets.Method)] class GlobalSetupAttribute : Attribute { }
[AttributeUsage(AttributeTargets.Method)] class BenchmarkAttribute : Attribute { public bool Baseline { get; set; } }
[AttributeUsage(AttributeTargets.Property)] class ParamsAttribute(params object[] values) : Attribute
{
    public object[] Values { get; } = values;
}

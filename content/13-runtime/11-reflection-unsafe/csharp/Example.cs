using System.Reflection;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;

// Reflection: тип, свойства, атрибуты — аналог reflect.Type, Field, Tag.
var type = typeof(Config); // ↔ reflect.TypeFor[Config]()
foreach (var p in type.GetProperties())
{
    var env = p.GetCustomAttribute<EnvAttribute>(); // атрибут — объект, тег Go — строка
    Console.WriteLine($"{p.Name} {p.PropertyType.Name} env={env?.Name} required={env?.Required}");
}

// Установка значения: в Go нужно Elem() от указателя и экспортированное поле.
var cfg = new Config();
type.GetProperty(nameof(Config.Port))!.SetValue(cfg, 8080);
Console.WriteLine(cfg.Port);

// В .NET private-члены доступны через BindingFlags.NonPublic —
// в Go неэкспортированные поля reflect не меняет никогда.
type.GetField("secret", BindingFlags.NonPublic | BindingFlags.Instance)!.SetValue(cfg, "s3cr3t");
Console.WriteLine(cfg.HasSecret);

// Размер и раскладка: Unsafe.SizeOf ↔ unsafe.Sizeof.
Console.WriteLine($"{Unsafe.SizeOf<Padded>()} {Unsafe.SizeOf<Packed>()}"); // 24 16

// Переинтерпретация памяти без ключевого слова unsafe ↔ шаблон 1 и unsafe.Slice.
double f = 1.5;
long bits = Unsafe.As<double, long>(ref f);
Console.WriteLine(bits == BitConverter.DoubleToInt64Bits(f));
ushort[] nums = { 0x0102, 0x0304 };
Span<byte> view = MemoryMarshal.AsBytes(nums.AsSpan());
Console.WriteLine(view.Length); // 4

[AttributeUsage(AttributeTargets.Property)]
sealed class EnvAttribute(string name, bool required = false) : Attribute
{
    public string Name { get; } = name;
    public bool Required { get; } = required;
}

sealed class Config
{
    [Env("HOST")] public string Host { get; set; } = "localhost";
    [Env("PORT", required: true)] public int Port { get; set; }
    [Env("TIMEOUT")] public TimeSpan Timeout { get; set; }
    private string? secret = null; // меняется только рефлексией
    public bool HasSecret => secret is not null;
}

// Sequential-раскладка, как в Go: порядок полей влияет на размер.
[StructLayout(LayoutKind.Sequential)]
struct Padded { public bool A; public long B; public bool C; }

[StructLayout(LayoutKind.Sequential)]
struct Packed { public long B; public bool A; public bool C; }

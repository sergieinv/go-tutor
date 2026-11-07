// xUnit: [Theory] + [InlineData] — та же таблица, только в атрибутах.
// Чтобы файл собирался без NuGet, атрибуты и Assert объявлены заглушками ниже,
// а «раннер» читает атрибуты через reflection — как это делает xUnit.
using System.Reflection;
using System.Text.RegularExpressions;

foreach (var m in typeof(SlugTests).GetMethods())
    foreach (var data in m.GetCustomAttributes<InlineDataAttribute>())
    {
        // Имя случая xUnit строит из значений аргументов; в Go его задаёт поле name.
        Console.WriteLine($"{m.Name}({string.Join(", ", data.Values)})");
        m.Invoke(new SlugTests(), data.Values);
    }

public static class Text
{
    public static string Slug(string title) =>
        Regex.Replace(title.ToLowerInvariant(), @"[^\p{L}\p{Nd}]+", "-").Trim('-');
}

public class SlugTests
{
    // В атрибут попадают только константы. Сложные случаи (объекты, ошибки, коллекции)
    // уходят в [MemberData]/TheoryData<>; в Go таблица — обычный литерал среза структур.
    [Theory]
    [InlineData("Hello World", "hello-world")]
    [InlineData("  Go  ", "go")]
    [InlineData("Go 1.25", "go-1-25")]
    public void Slug(string title, string expected)
    {
        // Порядок xUnit: expected, actual. В Go принято наоборот: got, want.
        Assert.Equal(expected, Text.Slug(title));
    }
}

// --- заглушки вместо пакета xunit ---
[AttributeUsage(AttributeTargets.Method)]
public class TheoryAttribute : Attribute;

[AttributeUsage(AttributeTargets.Method, AllowMultiple = true)]
public class InlineDataAttribute(params object[] values) : Attribute
{
    public object[] Values { get; } = values;
}

public static class Assert
{
    public static void Equal<T>(T expected, T actual)
    {
        if (!EqualityComparer<T>.Default.Equals(expected, actual))
            throw new Exception($"Assert.Equal() Failure: Expected {expected}, Actual {actual}");
    }
}

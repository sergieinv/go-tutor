// Shop.csproj — аналог go.mod:
// <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />

using System.Text.Json;
using Str = System.String; // псевдоним, как str "strings"

namespace Shop.Orders; // логическое имя, с каталогом не связано

public class Order
{
    // Видимость — модификаторы, а не регистр
    public int Id { get; set; }
    public string Customer { get; set; } = "";
    private int discount; // System.Text.Json тоже не увидит

    // Статический конструктор — ближайший аналог init()
    static Order() => Console.WriteLine("static ctor");
}

// internal — видно только внутри сборки;
// в Go эту роль играет каталог internal/
internal static class Money
{
    public static long Sum(long[] xs) => xs.Sum();
}

// Точка входа — как package main + func main()
internal static class Program
{
    private static void Main()
    {
        var o = new Order { Id = 1, Customer = "ann" }; // сначала static ctor
        Console.WriteLine(JsonSerializer.Serialize(o)); // {"Id":1,"Customer":"ann"}
        Console.WriteLine(Money.Sum([100, 250]));
        Console.WriteLine(Str.Join(",", "a", "b"));
    }
}

// [assembly: InternalsVisibleTo("Shop.Tests")] —
// в Go тесты лежат в том же пакете и видят всё без атрибутов

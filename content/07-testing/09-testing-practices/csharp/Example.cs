// В .NET тесты живут в отдельном проекте Orders.Tests (xUnit/NUnit из NuGet),
// моки обычно делает Moq, а категории фильтрует dotnet test --filter.
// Чтобы файл собирался без пакетов, атрибуты xUnit объявлены заглушками внизу.
var t = new OrderServiceTests();
t.Place_SavesOrder();
foreach (var (customer, total) in new[] { ("  ", 100), ("bob", 0) })
    t.Place_Invalid_Throws(customer, total);
Console.WriteLine("ok");

public sealed record Order(string Id, string Customer, long Total, DateTimeOffset Created);

public interface IOrderRepo
{
    void Save(Order o);
    IReadOnlyList<Order> ByCustomer(string customer);
}

// Время и id внедряются — TimeProvider (.NET 8) ≈ now func() time.Time.
public sealed class OrderService(IOrderRepo repo, TimeProvider clock, Func<string> newId)
{
    public Order Place(string customer, long total)
    {
        customer = customer.Trim();
        if (customer.Length == 0 || total <= 0)
            throw new ArgumentException($"неверный заказ: {customer}, {total}");
        var o = new Order(newId(), customer, total, clock.GetUtcNow());
        repo.Save(o);
        return o;
    }
}

// --- Orders.Tests (в Go — orders_test.go рядом с кодом) ---

// Фейк руками вместо Mock<IOrderRepo>().Verify(r => r.Save(...), Times.Once).
public sealed class FakeRepo : IOrderRepo
{
    public readonly Dictionary<string, Order> ById = new();
    public void Save(Order o) => ById.Add(o.Id, o);
    public IReadOnlyList<Order> ByCustomer(string c) => ById.Values.Where(o => o.Customer == c).ToList();
}

public sealed class FixedClock(DateTimeOffset now) : TimeProvider
{
    public override DateTimeOffset GetUtcNow() => now;
}

public class OrderServiceTests
{
    static readonly DateTimeOffset T0 = new(2025, 3, 1, 10, 0, 0, TimeSpan.Zero);

    // Builder с разумными значениями по умолчанию.
    static Order AnOrder(long total = 100_00) => new("ord-1", "alice", total, T0);

    static OrderService NewService(IOrderRepo repo)
    {
        var n = 0;
        return new OrderService(repo, new FixedClock(T0), () => $"ord-{++n}");
    }

    [Fact] // в Go — func TestPlace(t *testing.T)
    public void Place_SavesOrder()
    {
        var repo = new FakeRepo();
        var got = NewService(repo).Place(" alice ", 250_00);
        var want = AnOrder(total: 250_00);
        if (got != want) throw new Exception($"Place = {got}, want {want}");
        if (repo.ById[want.Id] != want) throw new Exception("заказ не сохранён");
    }

    [Theory] // в Go — table-driven тест с t.Run
    [InlineData("  ", 100)]
    [InlineData("bob", 0)]
    public void Place_Invalid_Throws(string customer, long total)
    {
        var repo = new FakeRepo();
        try { NewService(repo).Place(customer, total); }
        catch (ArgumentException) { return; }
        throw new Exception($"Place({customer}, {total}): ожидалось исключение");
    }

    [Fact, Trait("Category", "Integration")] // в Go — build tag, -short или переменная окружения
    public void Repo_Postgres()
    {
        var dsn = Environment.GetEnvironmentVariable("ORDERS_TEST_DSN");
        if (string.IsNullOrEmpty(dsn)) return; // в xUnit v3 — Assert.Skip(...)
    }
}

// Заглушки атрибутов xUnit, чтобы пример собирался без NuGet.
[AttributeUsage(AttributeTargets.Method)] public class FactAttribute : Attribute { }
[AttributeUsage(AttributeTargets.Method)] public class TheoryAttribute : FactAttribute { }
[AttributeUsage(AttributeTargets.Method, AllowMultiple = true)]
public class InlineDataAttribute(params object[] data) : Attribute { public object[] Data { get; } = data; }
[AttributeUsage(AttributeTargets.Method, AllowMultiple = true)]
public class TraitAttribute(string name, string value) : Attribute
{
    public string Name { get; } = name;
    public string Value { get; } = value;
}

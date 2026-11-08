// Контрактные тесты в xUnit — абстрактный базовый класс с [Fact], наследники
// подставляют реализацию. В Go то же делает функция testStore(t, newStore).
// Атрибуты объявлены заглушками внизу, чтобы файл собирался без NuGet.

var tests = new InMemoryStockStoreTests();
tests.TakeMoreThanAvailableFails();
Console.WriteLine("контракт пройден фейком");

public interface IStockStore
{
    void Add(string sku, int qty);
    void Take(string sku, int qty);
    int Qty(string sku);
}

public class InsufficientStockException : Exception;

// Фейк вместо Mock<IStockStore>().Setup(...).Verify(...): рабочая реализация,
// которую проверяет тот же контракт, что и настоящую.
public class InMemoryStockStore : IStockStore
{
    private readonly Dictionary<string, int> _qty = new();
    public void Add(string sku, int qty) => _qty[sku] = _qty.GetValueOrDefault(sku) + qty;
    public int Qty(string sku) => _qty.TryGetValue(sku, out var q) ? q : throw new KeyNotFoundException(sku);
    public void Take(string sku, int qty)
    {
        if (Qty(sku) < qty) throw new InsufficientStockException();
        _qty[sku] -= qty;
    }
}

public abstract class StockStoreContract
{
    protected abstract IStockStore CreateStore();

    [Fact]
    public void TakeMoreThanAvailableFails()
    {
        var store = CreateStore();
        store.Add("lamp", 1);
        try
        {
            store.Take("lamp", 2);
            throw new Exception("ожидалось InsufficientStockException");
        }
        catch (InsufficientStockException) { }
        if (store.Qty("lamp") != 1) throw new Exception("остаток изменился");
    }
}

public class InMemoryStockStoreTests : StockStoreContract
{
    protected override IStockStore CreateStore() => new InMemoryStockStore();
}

// Интеграционный прогон того же контракта. В .NET базу поднимают
// Testcontainers или WebApplicationFactory + реальная строка подключения,
// транзакцию на тест — через TransactionScope, который откатывается в Dispose.
// В Go TransactionScope нет: транзакция передаётся явно (DBTX) и
// откатывается в t.Cleanup.
[Trait("Category", "Integration")]
public class PostgresStockStoreTests : StockStoreContract
{
    protected override IStockStore CreateStore() =>
        throw new NotSupportedException("нужен PostgreSQL: Skip, если нет строки подключения");
}

[AttributeUsage(AttributeTargets.Method)]
public sealed class FactAttribute : Attribute;

[AttributeUsage(AttributeTargets.Class)]
public sealed class TraitAttribute(string name, string value) : Attribute
{
    public string Name { get; } = name;
    public string Value { get; } = value;
}

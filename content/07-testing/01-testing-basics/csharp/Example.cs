// Обычно это два проекта: Shop и Shop.Tests (NuGet: xunit, xunit.runner.visualstudio,
// Microsoft.NET.Test.Sdk) плюс [InternalsVisibleTo("Shop.Tests")] для доступа к internal.
// Чтобы файл собирался без NuGet, атрибуты и Assert ниже объявлены заглушками,
// а «раннер» — несколько строк вызова методов.
var tests = new DiscountTests();
tests.Discount_AppliesPercent();
tests.Discount_RejectsBadPercent();
Console.WriteLine("ok");

public static class Shop
{
    public static int Discount(int price, int percent)
    {
        if (percent is < 0 or > 100)
            throw new ArgumentOutOfRangeException(nameof(percent));
        return price - price * percent / 100;
    }
}

// В Go — функции TestXxx в файле shop_test.go того же каталога, без класса и атрибутов.
public class DiscountTests
{
    [Fact]
    public void Discount_AppliesPercent()
    {
        // Assert.Equal бросает исключение — тест обрывается, как после t.Fatal.
        // В Go по умолчанию t.Errorf: тест отмечен упавшим, но продолжается.
        Assert.Equal(850, Shop.Discount(1000, 15));
    }

    [Fact(Skip = "нужна база данных")] // ≈ t.Skip, но решение статическое; в рантайме — Assert.Skip (xUnit v3)
    public void Discount_WithDatabase() { }

    [Fact]
    public void Discount_RejectsBadPercent()
    {
        // Ошибка — исключение; в Go — значение, проверяемое errors.Is.
        Assert.Throws<ArgumentOutOfRangeException>(() => Shop.Discount(1000, 101));
    }
}

// --- заглушки вместо пакета xunit ---
[AttributeUsage(AttributeTargets.Method)]
public class FactAttribute : Attribute
{
    public string? Skip { get; set; }
}

public static class Assert
{
    public static void Equal<T>(T expected, T actual)
    {
        if (!EqualityComparer<T>.Default.Equals(expected, actual))
            throw new Exception($"Assert.Equal() Failure: Expected {expected}, Actual {actual}");
    }

    public static void Throws<TException>(Action action) where TException : Exception
    {
        try { action(); }
        catch (TException) { return; }
        throw new Exception($"Assert.Throws() Failure: {typeof(TException).Name} не брошено");
    }
}

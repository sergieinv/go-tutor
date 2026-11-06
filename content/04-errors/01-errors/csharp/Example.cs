// В C# ожидаемый сбой — исключение, и сигнатура о нём молчит
static int ParseAge(string s)
{
    int n;
    try
    {
        n = int.Parse(s); // FormatException
    }
    catch (FormatException e)
    {
        // Контекст добавляют, оборачивая исключение
        throw new ArgumentException($"разбор возраста \"{s}\"", e);
    }
    if (n < 0) throw new ArgumentOutOfRangeException(nameof(s), "возраст не может быть отрицательным");
    return n;
}

foreach (var s in new[] { "42", "abc", "-5" })
{
    try
    {
        Console.WriteLine($"возраст: {ParseAge(s)}");
    }
    catch (ArgumentException e) // ловим где-то выше по стеку
    {
        Console.WriteLine($"ошибка: {e.Message}");
    }
}

// Ближе всего к стилю Go — паттерн TryParse: успех явно в результате
if (!int.TryParse("12a", out var n)) Console.WriteLine("не число");

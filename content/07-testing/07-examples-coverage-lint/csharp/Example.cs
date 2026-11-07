// В C# пример в XML-документации — просто текст: компилятор не проверит,
// что он собирается и печатает написанное. В Go Example-функция — тест.
// Покрытие — coverlet:   dotnet test --collect:"XPlat Code Coverage"
//                        reportgenerator -reports:**/coverage.cobertura.xml -targetdir:report
// Анализаторы — Roslyn (NetAnalyzers, StyleCop) с настройкой в .editorconfig:
//   [*.cs]
//   dotnet_diagnostic.CA2000.severity = error   # IDisposable без Dispose
//   dotnet_diagnostic.CA1305.severity = warning # строка без IFormatProvider
using System.Globalization;

Console.WriteLine(new Amount(1234));        // 12.34
Console.WriteLine(new Amount(-5));          // -0.05
Console.WriteLine(string.Join(" ", new Amount(1000).Split(3))); // 3.34 3.33 3.33

/// <summary>Сумма в копейках.</summary>
/// <example>
/// Этот код никто не запустит — он может давно не совпадать с реальностью:
/// <code>
/// Console.WriteLine(new Amount(1234)); // 12.34
/// </code>
/// </example>
public readonly record struct Amount(long Kopecks)
{
    // Как и в Go, -5 / 100 == 0 и -5 % 100 == -5 — знак выводим отдельно.
    public override string ToString()
    {
        var sign = Kopecks < 0 ? "-" : "";
        var abs = (ulong)Math.Abs((decimal)Kopecks);
        return string.Create(CultureInfo.InvariantCulture, $"{sign}{abs / 100}.{abs % 100:D2}");
    }

    public Amount[] Split(int n)
    {
        if (n <= 0) return [];
        var (q, r) = Math.DivRem(Kopecks, n);
        return Enumerable.Range(0, n).Select(i => new Amount(q + (i < r ? 1 : 0))).ToArray();
    }
}

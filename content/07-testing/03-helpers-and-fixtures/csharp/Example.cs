// xUnit: фикстура — конструктор и Dispose тестового класса, общая на класс — IClassFixture<T>.
// В Go это хелпер newStore(t), который сам регистрирует t.Cleanup.
// Атрибут [Fact] объявлен заглушкой ниже, чтобы файл собирался без NuGet.
using var test = new StoreTests();
test.Save_WritesFile();
Console.WriteLine("ok");

public class StoreTests : IDisposable
{
    private readonly string _dir;
    private readonly string? _oldTitle;

    // Конструктор (в NUnit — [SetUp]) ≈ хелпер + t.TempDir() + t.Setenv(...)
    public StoreTests()
    {
        _dir = Directory.CreateTempSubdirectory("report-").FullName;
        _oldTitle = Environment.GetEnvironmentVariable("REPORT_TITLE");
        Environment.SetEnvironmentVariable("REPORT_TITLE", "Склад");
    }

    [Fact]
    public void Save_WritesFile()
    {
        var path = Path.Combine(_dir, "out.txt");
        File.WriteAllText(path, Environment.GetEnvironmentVariable("REPORT_TITLE"));
        // Snapshot-библиотеки (Verify) сравнили бы с Save_WritesFile.verified.txt —
        // в Go это golden-файл в testdata/ и флаг -update.
        if (File.ReadAllText(path) != "Склад")
            throw new Exception("неверное содержимое");
    }

    // Dispose (в NUnit — [TearDown]) ≈ функции t.Cleanup. Порядок уборки — ваша забота;
    // в Go Cleanup выполняются автоматически в обратном порядке регистрации.
    public void Dispose()
    {
        Environment.SetEnvironmentVariable("REPORT_TITLE", _oldTitle);
        Directory.Delete(_dir, recursive: true);
    }
}

// --- заглушка вместо пакета xunit ---
[AttributeUsage(AttributeTargets.Method)]
public class FactAttribute : Attribute;

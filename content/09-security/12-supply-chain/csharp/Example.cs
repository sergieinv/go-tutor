// Аналог задачи урока для NuGet: проверка packages.lock.json по локальной
// базе уязвимостей. В реальном проекте это делают NuGet Audit при
// `dotnet restore` и `dotnet list package --vulnerable` — на уровне пакета,
// без анализа вызовов (в Go govulncheck доходит до функции и трассы).
using System.Text.Json;

// <RestorePackagesWithLockFile>true</RestorePackagesWithLockFile> и
// <RestoreLockedMode>true</RestoreLockedMode> в CI ≈ точные версии go.mod
// и проверка go.sum: restore падает, если граф разошёлся с lock-файлом.
const string lockJson = """
{
  "version": 1,
  "dependencies": {
    "net10.0": {
      "Newtonsoft.Json": { "type": "Direct", "requested": "[12.0.1, )", "resolved": "12.0.1" },
      "System.Text.Encodings.Web": { "type": "Transitive", "resolved": "4.5.0" }
    }
  }
}
""";

var db = new[]
{
    new Vuln("GHSA-5crp-9r3c-p9vr", "Newtonsoft.Json", Introduced: null, Fixed: "13.0.1"),
};

using var doc = JsonDocument.Parse(lockJson);
foreach (var framework in doc.RootElement.GetProperty("dependencies").EnumerateObject())
foreach (var package in framework.Value.EnumerateObject())
{
    var resolved = package.Value.GetProperty("resolved").GetString()!;
    // System.Version не знает пререлизов: "13.0.1-beta1" не разберётся.
    // NuGet.Versioning.NuGetVersion — как semver.Compare в Go.
    var version = Version.Parse(resolved);
    foreach (var v in db.Where(v => v.Package == package.Name))
    {
        bool afterIntro = v.Introduced is null || version >= Version.Parse(v.Introduced);
        bool beforeFix = v.Fixed is null || version < Version.Parse(v.Fixed);
        if (afterIntro && beforeFix)
            Console.WriteLine($"{v.Id} {package.Name}@{resolved}");
    }
}

record Vuln(string Id, string Package, string? Introduced, string? Fixed);

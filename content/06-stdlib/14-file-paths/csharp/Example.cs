// Один класс System.IO.Path вместо двух пакетов path и path/filepath
var p = Path.Combine("data", "reports", "q1.tar.gz");

Console.WriteLine(Path.GetDirectoryName(p));             // filepath.Dir
Console.WriteLine(Path.GetFileName(p));                  // filepath.Base
Console.WriteLine(Path.GetExtension(p));                 // filepath.Ext → ".gz"
Console.WriteLine(Path.GetFileNameWithoutExtension(p));  // TrimSuffix(Base, Ext)
Console.WriteLine(Path.GetRelativePath("project/src", "project/docs/readme.md")); // filepath.Rel
Console.WriteLine(Path.IsPathFullyQualified("data"));   // filepath.IsAbs

// Отличие: абсолютный второй аргумент отбрасывает первый.
// filepath.Join("a", "/b") в Go вернёт "a/b".
Console.WriteLine(Path.Combine("a", "/b"));              // "/b"

// Combine не нормализует ".." — в Go это делает Join через Clean
Console.WriteLine(Path.Combine("data", "..", "x"));      // "data/../x"
Console.WriteLine(Path.GetFullPath(Path.Combine("data", "..", "x"))); // filepath.Abs

// Поиск по шаблону: Directory.GetFiles ≈ filepath.Glob.
// Но для несуществующего каталога GetFiles бросит исключение, а Glob вернёт пустой срез.
foreach (var f in Directory.Exists("logs") ? Directory.GetFiles("logs", "*.log") : [])
{
    Console.WriteLine(f);
}
Console.WriteLine(Path.DirectorySeparatorChar);          // filepath.Separator

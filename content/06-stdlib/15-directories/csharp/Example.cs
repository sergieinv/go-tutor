using System.IO.Enumeration;

// CreateDirectory создаёт всю цепочку — как os.MkdirAll
Directory.CreateDirectory("project/internal/db");
File.WriteAllText("project/main.go", "x");
File.WriteAllText("project/internal/db/db.go", "x");

// Один уровень — os.ReadDir (но порядок здесь не гарантирован)
foreach (var entry in new DirectoryInfo("project").EnumerateFileSystemInfos())
{
    Console.WriteLine($"{entry.Name} dir={entry is DirectoryInfo}");
}

// Рекурсивный обход: EnumerateFiles(..., AllDirectories) — плоский перечислитель,
// фильтр по пути всё равно заходит внутрь каталога. Не заходить в поддерево
// (как fs.SkipDir) умеет только низкоуровневый FileSystemEnumerable.
var walk = new FileSystemEnumerable<string>("project",
    (ref FileSystemEntry e) => e.ToFullPath(),
    new EnumerationOptions { RecurseSubdirectories = true })
{
    ShouldIncludePredicate = (ref FileSystemEntry e) => !e.IsDirectory,
    ShouldRecursePredicate = (ref FileSystemEntry e) => e.FileName is not ".git" and not "node_modules",
};
foreach (var path in walk.Order())
{
    Console.WriteLine(Path.GetRelativePath("project", path));
}

// Абстракции ФС в BCL нет — для тестов обычно берут System.IO.Abstractions из NuGet.
// В Go это встроенный интерфейс fs.FS: os.DirFS, embed.FS, fstest.MapFS.

// os.RemoveAll; отличие — для несуществующего каталога будет исключение, а RemoveAll вернёт nil
Directory.Delete("project", recursive: true);
Console.WriteLine(Directory.Exists("project")); // False

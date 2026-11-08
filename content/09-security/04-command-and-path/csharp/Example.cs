using System.Diagnostics;
using System.IO.Compression;

var userFileName = "-delete"; // пришло от пользователя

// Аргументы — списком: ArgumentList передаёт каждый элемент как есть,
// как argv в exec.Command. Оболочки нет (UseShellExecute = false).
var psi = new ProcessStartInfo("archive") { UseShellExecute = false };
psi.ArgumentList.Add("--");          // конец флагов — как в Go
psi.ArgumentList.Add(userFileName);  // "-delete" останется именем файла
// Свойство Arguments — одна строка, которую потом разбирают по правилам
// командной строки: кавычки и пробелы во вводе меняют число аргументов.
Process.Start(psi)?.WaitForExit();

// Path traversal: Path.Combine/Join, как filepath.Join, не защищают.
// Проверка через полный путь ловит "..", но не симлинки:
// аналога os.Root (открытие относительно дескриптора каталога) в .NET нет.
string ReadPublic(string baseDir, string name)
{
    var root = Path.GetFullPath(baseDir) + Path.DirectorySeparatorChar;
    var full = Path.GetFullPath(Path.Join(root, name));
    if (!full.StartsWith(root, StringComparison.Ordinal))
        throw new UnauthorizedAccessException("путь вне каталога");
    // full всё ещё может быть симлинком наружу — проверьте
    // new FileInfo(full).LinkTarget или откажитесь от симлинков.
    return File.ReadAllText(full);
}

// ZipFile.ExtractToDirectory сам бросает IOException на записи
// вида "../x" — в Go archive/zip такой проверки при распаковке нет:
// её делаете вы (filepath.IsLocal или запись через os.Root).
ZipFile.ExtractToDirectory("upload.zip", "public");

Console.WriteLine(ReadPublic("public", "hello.txt"));

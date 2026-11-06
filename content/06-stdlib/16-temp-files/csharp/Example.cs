// GetTempFileName создаёт пустой файл tmpXXXX.tmp — имя не настраивается.
// os.CreateTemp("", "upload-*.csv") принимает шаблон и возвращает открытый файл.
var path = Path.GetTempFileName();
try
{
    File.WriteAllText(path, "id,name\n");
}
finally
{
    File.Delete(path); // в Go — defer os.Remove(f.Name())
}

// DeleteOnClose: файл удалится при закрытии. В Go такого флага нет.
using (var fs = new FileStream(Path.Combine(Path.GetTempPath(), Path.GetRandomFileName()),
           FileMode.CreateNew, FileAccess.ReadWrite, FileShare.None, 4096, FileOptions.DeleteOnClose))
{
    fs.WriteByte(1);
}

// .NET 7+: временный каталог с префиксом — os.MkdirTemp("", "build-")
DirectoryInfo dir = Directory.CreateTempSubdirectory("build-");
try
{
    File.WriteAllBytes(Path.Combine(dir.FullName, "out.bin"), [1, 2, 3]);
}
finally
{
    dir.Delete(recursive: true); // defer os.RemoveAll(dir)
}

// В тестах Go каталог даёт t.TempDir() и удаляет его сам;
// в xUnit обычно пишут IDisposable-фикстуру с Directory.Delete в Dispose.

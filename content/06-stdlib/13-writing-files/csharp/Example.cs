// Записать всё сразу — os.WriteFile
File.WriteAllText("log.txt", "start\n");

// Дописать в конец — OpenFile(O_WRONLY|O_APPEND|O_CREATE)
File.AppendAllText("log.txt", "append 1\n");

// FileMode.CreateNew — аналог O_CREATE|O_EXCL: исключение, если файл есть
try
{
    using var _ = new FileStream("log.txt", FileMode.CreateNew);
}
catch (IOException)
{
    Console.WriteLine("файл уже существует");
}

// StreamWriter буферизует и сбрасывает буфер в Dispose.
// В Go у bufio.Writer нет Close — Flush нужен явно.
using (var w = new StreamWriter("data.txt"))
{
    for (var i = 0; i < 3; i++)
    {
        w.WriteLine($"строка {i}");
    }
} // здесь Flush + Close; ошибка закрытия станет исключением

// Атомарная замена: временный файл рядом с целевым и Move с перезаписью
var tmp = "config.json.tmp";
File.WriteAllText(tmp, """{"v":1}""");
File.Move(tmp, "config.json", overwrite: true);
Console.WriteLine(File.ReadAllText("config.json"));

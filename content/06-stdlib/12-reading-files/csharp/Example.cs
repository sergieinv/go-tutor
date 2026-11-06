const string name = "notes.txt";
File.WriteAllText(name, "первая строка\nвторая строка\r\nтретья\n");

// Целиком: в Go — os.ReadFile, но результат там []byte, а не string
byte[] data = File.ReadAllBytes(name);
Console.WriteLine($"ReadAllBytes: {data.Length} байт");

// Построчно: StreamReader вместо bufio.Scanner.
// Отсутствие файла — исключение FileNotFoundException, а не значение ошибки.
using (var reader = new StreamReader(name))
{
    var i = 1;
    while (reader.ReadLine() is { } line) // предела длины строки нет
    {
        Console.WriteLine($"{i++}: {line}");
    }
}

// Stream — абстрактный класс; в Go роль играет интерфейс io.Reader из одного метода
static int CountWords(TextReader r) =>
    r.ReadToEnd().Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries).Length;

using (var f = File.OpenText(name))
{
    Console.WriteLine($"слов в файле: {CountWords(f)}, в строке: {CountWords(new StringReader("раз два три"))}");
}

File.Delete(name);

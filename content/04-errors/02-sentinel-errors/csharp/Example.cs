// В C# вид сбоя кодируют типом исключения
static string FindUser(int id) =>
    id switch
    {
        1 => "alice",
        2 => "bob",
        _ => throw new NotFoundException($"user {id}"),
    };

foreach (var id in new[] { 1, 3 })
{
    try
    {
        Console.WriteLine($"{id} — {FindUser(id)}");
    }
    catch (NotFoundException) // аналог errors.Is(err, ErrNotFound)
    {
        Console.WriteLine($"{id} — нет такого пользователя");
    }
}

// Конец потока в .NET — не исключение, а особый результат
using var reader = new StringReader("a\nb");
while (reader.ReadLine() is { } line) // null — аналог io.EOF
    Console.WriteLine(line);

// Вид сбоя — свой тип исключения (объявления типов — после top-level кода)
class NotFoundException(string message) : Exception(message);

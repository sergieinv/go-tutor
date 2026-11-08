using System.Text.Json;
using System.Text.Json.Serialization;

// JsonSerializerOptions.Strict (.NET 10): неизвестные поля и дубли ключей — ошибка,
// имена — с учётом регистра. В Go дубли не ловятся, а регистр не учитывается.
var strict = JsonSerializerOptions.Strict;

foreach (var body in new[]
{
    """{"to":"bob","amount":500}""",
    """{"to":"bob","amount":500,"fee":0}""",                // UnmappedMemberHandling.Disallow
    """{"to":"bob","amount":5} {"to":"eve","amount":9000}""", // хвост — ошибка и по умолчанию
    """{"to":"bob","amount":5,"amount":-9000}""",            // AllowDuplicateProperties = false
})
{
    try
    {
        var t = JsonSerializer.Deserialize<Transfer>(body, strict)!;
        Console.WriteLine($"ok {t.Amount}");
    }
    catch (JsonException) // в Go — ошибка от Decode или dec.Token()
    {
        Console.WriteLine("400");
    }
}

// Переполнение: по умолчанию C# тоже молча заворачивает (unchecked),
// но checked превращает его в исключение. В Go checked нет — границы до умножения.
long qty = (1L << 62) + 1;
try { Console.WriteLine(checked(qty * 4)); }
catch (OverflowException) { Console.WriteLine("overflow"); }

// Усечение: явное приведение (int) отбрасывает старшие биты, как int32(x) в Go;
// int.Parse бросает OverflowException — как ParseInt(s, 10, 32) с ErrRange.
long id64 = 4294967297;
Console.WriteLine((int)id64); // 1
Console.WriteLine(int.TryParse("4294967297", out _)); // False

// string.Length — UTF-16-единицы: "Ёжик".Length == 4, но эмодзи "👍".Length == 2.
Console.WriteLine($"{"Ёжик".Length} {"👍".Length}");

record Transfer(
    [property: JsonPropertyName("to")] string To,
    [property: JsonPropertyName("amount")] long Amount);

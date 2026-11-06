// В C# свой тип ошибки — наследник Exception
static void ValidateName(string name)
{
    if (name.Length == 0) throw new ValidationException("name", "пустое значение");
}

try
{
    ValidateName("");
}
catch (ValidationException e) // аналог errors.As(err, &ve)
{
    Console.WriteLine($"ошибка в поле {e.Field} ({e.Reason})");
}

// Ловушки с nil-интерфейсом в C# нет: null-ссылка — это просто null,
// и `(Exception?)null == null` всегда true.
ValidationException? ve = null;
Exception? err = ve;
Console.WriteLine(err == null); // True — в Go для error было бы false

// Свой тип ошибки: поля + сообщение для базового Exception
class ValidationException(string field, string reason)
    : Exception($"поле {field}: {reason}")
{
    public string Field { get; } = field;
    public string Reason { get; } = reason;
}

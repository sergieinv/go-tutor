// В C# обёртка — InnerException, цепочку обходят вручную
try
{
    try
    {
        throw new QueryException("SELECT users", new NotFoundException());
    }
    catch (Exception e)
    {
        throw new InvalidOperationException($"load profile 7: {e.Message}", e);
    }
}
catch (Exception e)
{
    // Аналог errors.Is: проход по InnerException
    for (var cur = e; cur != null; cur = cur.InnerException)
        if (cur is NotFoundException) Console.WriteLine("not found");
}

// Аналог errors.Join — AggregateException
var all = new AggregateException(new Exception("name: required"), new NotFoundException());
Console.WriteLine(all.InnerExceptions.Count); // 2

// Обёртка: причина уходит в InnerException через базовый конструктор
class QueryException(string query, Exception inner)
    : Exception($"{query}: {inner.Message}", inner);

class NotFoundException() : Exception("not found");

// В C# «граница», где сбой превращается в результат, — try/catch
static (int Result, Exception? Error) SafeCall(Func<int> f)
{
    try
    {
        return (f(), null);
    }
    catch (OutOfMemoryException)
    {
        throw; // re-throw: такие сбои не обрабатываем — аналог panic(r)
    }
    catch (Exception e)
    {
        return (0, new InvalidOperationException($"panic: {e.Message}", e));
    }
}

var arr = new[] { 1, 2, 3 };
Console.WriteLine(SafeCall(() => arr[5]).Error?.Message);
// panic: Index was outside the bounds of the array.

// Исключение в фоновой задаче не роняет процесс, пока задачу не дождались.
// В Go неперехваченная паника в горутине сразу завершает программу.
var task = Task.Run(() => throw new InvalidOperationException("boom"));
try { await task; } catch (InvalidOperationException e) { Console.WriteLine(e.Message); }

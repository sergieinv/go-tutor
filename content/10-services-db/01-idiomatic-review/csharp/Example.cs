// Те же правила в привычных для .NET формах: интерфейс рядом с реализацией
// (его регистрируют в DI-контейнере), CancellationToken последним параметром,
// исключение с InnerException вместо обёртки %w.
var repo = new UserRepository(new Dictionary<int, string> { [1] = "alice" });
var service = new UserService(repo);

try
{
    await service.GetUserNameAsync(2, CancellationToken.None);
}
// Аналог errors.Is: фильтр по типу исключения — и по InnerException.
catch (UserLookupException ex) when (ex.InnerException is NotFoundException)
{
    Console.WriteLine($"{ex.Message} <- {ex.InnerException.Message}");
}

using var cts = new CancellationTokenSource();
cts.Cancel();
try
{
    await service.GetUserNameAsync(1, cts.Token);
}
catch (OperationCanceledException)
{
    // Отмена — тоже исключение; в Go — ошибка context.Canceled.
    Console.WriteLine("отменено");
}

public class NotFoundException(string message) : Exception(message);

public class UserLookupException(string message, Exception inner) : Exception(message, inner);

// Интерфейс объявлен поставщиком, «I» в имени — соглашение .NET.
// В Go маленький интерфейс объявляет потребитель, и без префикса.
public interface IUserRepository
{
    Task<string> FindNameAsync(int id, CancellationToken ct);
}

public class UserRepository(Dictionary<int, string> names) : IUserRepository
{
    public Task<string> FindNameAsync(int id, CancellationToken ct)
    {
        ct.ThrowIfCancellationRequested(); // как ctx.Err() в начале Go-функции
        return names.TryGetValue(id, out var name)
            ? Task.FromResult(name)
            : throw new NotFoundException("not found");
    }
}

public class UserService(IUserRepository repo)
{
    // Get-префикс и PascalCase — норма для .NET. В Go: userName(ctx, id).
    public async Task<string> GetUserNameAsync(int id, CancellationToken ct)
    {
        try
        {
            return await repo.FindNameAsync(id, ct);
        }
        catch (NotFoundException ex)
        {
            // Без logger.LogError(ex, ...) здесь: «залогировать и пробросить»
            // даёт две записи об одном событии и в C#, и в Go.
            // Логирует тот, кто обработал.
            throw new UserLookupException($"user {id}", ex);
        }
    }
}

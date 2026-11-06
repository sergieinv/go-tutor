// Таймаут ожидания: гонка задачи с Task.Delay
async Task<string> FetchAsync(Func<Task<string>> work, TimeSpan timeout)
{
    var task = work();
    var winner = await Task.WhenAny(task, Task.Delay(timeout));
    if (winner != task)
        throw new TimeoutException(); // task продолжает работать
    return await task;
}

// То же короче (.NET 6+)
async Task<string> FetchShortAsync(Func<Task<string>> work, TimeSpan timeout) =>
    await work().WaitAsync(timeout);

// Настоящая отмена работы — CancellationTokenSource.CancelAfter.
// В Go для этого есть context.WithTimeout.
async Task<string> FetchWithCancelAsync(Func<CancellationToken, Task<string>> work, TimeSpan timeout)
{
    using var cts = new CancellationTokenSource();
    cts.CancelAfter(timeout);
    return await work(cts.Token); // work сама проверяет токен и прекращает работу
}

Console.WriteLine(await FetchAsync(() => Task.FromResult("данные"), TimeSpan.FromSeconds(1)));
try
{
    await FetchAsync(async () => { await Task.Delay(5000); return "поздно"; }, TimeSpan.FromMilliseconds(50));
}
catch (TimeoutException)
{
    Console.WriteLine("ошибка: таймаут");
}

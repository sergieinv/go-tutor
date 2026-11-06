// NuGet-пакет System.Threading.RateLimiting (.NET 7+); в ASP.NET Core уже подключён.
using System.Threading.RateLimiting;

// Равномерно: PeriodicTimer — аналог time.Ticker
using var timer = new PeriodicTimer(TimeSpan.FromMilliseconds(20));
for (var i = 1; i <= 3; i++)
{
    await timer.WaitForNextTickAsync(); // <-ticker.C
    Console.WriteLine($"равномерно: запрос {i}");
}

// Всплески: готовый token bucket из System.Threading.RateLimiting
using var limiter = new TokenBucketRateLimiter(new TokenBucketRateLimiterOptions
{
    TokenLimit = 3,                                       // burst — ёмкость ведра
    TokensPerPeriod = 1,
    ReplenishmentPeriod = TimeSpan.FromMilliseconds(20),  // interval
    QueueLimit = 10,
    QueueProcessingOrder = QueueProcessingOrder.OldestFirst,
    AutoReplenishment = true,
});

for (var i = 1; i <= 5; i++)
{
    using var lease = await limiter.AcquireAsync(); // ждём токен, как <-tokens
    Console.WriteLine($"со всплеском: запрос {i}");
}

// Попытка без ожидания — select с default в Go
using var attempt = limiter.AttemptAcquire();
Console.WriteLine(attempt.IsAcquired ? "пропущен" : "отклонён");

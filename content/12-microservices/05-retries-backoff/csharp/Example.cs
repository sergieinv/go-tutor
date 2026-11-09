// В .NET повторы обычно настраивают декларативно — Polly v8
// (пакет Polly.Core) или Microsoft.Extensions.Http.Resilience:
//
//   var pipeline = new ResiliencePipelineBuilder()
//       .AddRetry(new RetryStrategyOptions
//       {
//           ShouldHandle = new PredicateBuilder().Handle<RpcException>(e =>
//               e.StatusCode is StatusCode.Unavailable or StatusCode.ResourceExhausted),
//           BackoffType = DelayBackoffType.Exponential,
//           UseJitter = true,
//           MaxRetryAttempts = 4,
//           Delay = TimeSpan.FromMilliseconds(100),
//       })
//       .AddTimeout(TimeSpan.FromSeconds(2)) // общий бюджет времени
//       .Build();
//   await pipeline.ExecuteAsync(async ct => await client.ReserveAsync(req, cancellationToken: ct), ct);
//
//   // Для HttpClient — одной строкой: builder.Services.AddHttpClient<Api>().AddStandardResilienceHandler();
//
// Ниже — то же вручную, как в Go-примере, чтобы файл собирался без NuGet.
// RpcException и StatusCode — заглушки вместо типов из Grpc.Core.Api.

public enum StatusCode { OK, InvalidArgument, DeadlineExceeded, ResourceExhausted, Aborted, Unavailable }

public class RpcException(StatusCode code, TimeSpan? retryAfter = null) : Exception(code.ToString())
{
    public StatusCode StatusCode { get; } = code;
    // В настоящем Grpc.Core подсказку достают из трейлеров (google.rpc.RetryInfo
    // через пакет Google.Api.CommonProtos: e.GetRpcStatus()?.GetDetail<RetryInfo>()).
    public TimeSpan? RetryAfter { get; } = retryAfter;
}

public static class Retry
{
    static bool Retryable(RpcException e) =>
        e.StatusCode is StatusCode.Unavailable or StatusCode.ResourceExhausted or StatusCode.Aborted;

    public static async Task<T> RunAsync<T>(Func<CancellationToken, Task<T>> call, DateTime deadline, CancellationToken ct)
    {
        for (var attempt = 1; ; attempt++)
        {
            try
            {
                return await call(ct);
            }
            catch (RpcException e) when (Retryable(e) && attempt < 5)
            {
                // Полный джиттер: случайно в [0, min(100 мс · 2^(n−1), 2 с)).
                var cap = Math.Min(100 * Math.Pow(2, attempt - 1), 2000);
                var delay = e.RetryAfter ?? TimeSpan.FromMilliseconds(Random.Shared.NextDouble() * cap);
                if (DateTime.UtcNow + delay > deadline)
                    throw; // пауза переживёт дедлайн — вернуть ошибку сейчас
                await Task.Delay(delay, ct); // в отличие от time.Sleep, видит отмену
            }
        }
    }
}

public static class Program
{
    public static async Task Main()
    {
        var calls = 0;
        var result = await Retry.RunAsync(ct =>
        {
            calls++;
            if (calls < 3) throw new RpcException(StatusCode.Unavailable);
            return Task.FromResult("r-apple");
        }, DateTime.UtcNow.AddSeconds(5), CancellationToken.None);
        Console.WriteLine($"{result} после {calls} попыток");
    }
}

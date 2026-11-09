// ASP.NET Core: ограничитель параллелизма с очередью — готовое middleware
// (Microsoft.AspNetCore.RateLimiting поверх System.Threading.RateLimiting).
// В Go то же самое — middleware из двадцати строк на semaphore.Weighted.
using System.Threading.Channels;
using System.Threading.RateLimiting;
using Microsoft.AspNetCore.RateLimiting;

public static class Program
{
    public static void Main(string[] args)
    {
        var builder = WebApplication.CreateBuilder(args);
        builder.Services.AddRateLimiter(o =>
        {
            // По умолчанию отказ — 503; Retry-After ставим сами:
            // ConcurrencyLimiter не знает, когда освободится место.
            o.RejectionStatusCode = StatusCodes.Status503ServiceUnavailable;
            o.OnRejected = (ctx, _) =>
            {
                ctx.HttpContext.Response.Headers.RetryAfter = "1";
                return ValueTask.CompletedTask;
            };
            o.AddConcurrencyLimiter("orders", c =>
            {
                c.PermitLimit = 2;  // N одновременных
                c.QueueLimit = 2;   // M в очереди, остальным — отказ сразу
                // NewestFirst — LIFO: при полной очереди вытесняется самый старый.
                c.QueueProcessingOrder = QueueProcessingOrder.OldestFirst;
            });
        });

        var app = builder.Build();
        app.UseRateLimiter();
        // Ожидание в очереди middleware ограничивает только отменой запроса
        // (RequestAborted), своего таймаута очереди у него нет.
        app.MapPost("/orders", async (CancellationToken ct) =>
        {
            await Task.Delay(50, ct); // «долгий» запрос в базу
            return Results.Ok("заказ создан");
        }).RequireRateLimiting("orders");
        // Проверки здоровья — мимо ограничителя.
        app.MapGet("/healthz", () => "ok");

        // Backpressure внутри процесса: ограниченный канал ≈ буферизованный
        // chan в Go. Wait — отправитель ждёт, DropWrite — новое выбрасывается.
        var jobs = Channel.CreateBounded<int>(new BoundedChannelOptions(100)
        {
            FullMode = BoundedChannelFullMode.Wait,
        });
        _ = jobs;

        app.Run();
    }
}

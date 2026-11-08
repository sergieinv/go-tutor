using Microsoft.AspNetCore.Diagnostics;

var builder = WebApplication.CreateBuilder(args);
// Фоновый воркер — IHostedService; Generic Host остановит его сам после Kestrel,
// не дольше ShutdownTimeout (по умолчанию 30 с). В Go — errgroup и явный порядок.
builder.Services.AddHostedService<OutboxRelay>();
builder.Services.AddSingleton<StockService>();
// Единое место перевода исключений в статусы — IExceptionHandler (.NET 8+).
builder.Services.AddExceptionHandler<DomainExceptionHandler>();
builder.Services.AddProblemDetails();
var app = builder.Build();
app.UseExceptionHandler();

// RequestAborted — аналог r.Context(): отменяется, когда клиент ушёл.
app.MapPost("/items/{id:long}/reserve", async (long id, int qty, StockService svc, HttpContext http) =>
{
    using var cts = CancellationTokenSource.CreateLinkedTokenSource(http.RequestAborted);
    cts.CancelAfter(TimeSpan.FromSeconds(2)); // свой дедлайн, как context.WithTimeout
    await svc.ReserveAsync(id, qty, cts.Token);
    return Results.NoContent();
});
// app.Run() — в сервисе; здесь не вызываем, чтобы пример завершался.

public class NotFoundException(string message) : Exception(message);
public class ConflictException(string message) : Exception(message);

public class StockService
{
    public async Task ReserveAsync(long id, int qty, CancellationToken ct)
    {
        if (qty <= 0) throw new ArgumentOutOfRangeException(nameof(qty), "must be positive");
        // Токен нужно передать в каждый вызов вручную — как ctx в Go.
        await Task.Delay(10, ct);
        if (id != 1) throw new NotFoundException($"item {id}");
    }
}

public class DomainExceptionHandler(ILogger<DomainExceptionHandler> log) : IExceptionHandler
{
    public async ValueTask<bool> TryHandleAsync(HttpContext http, Exception ex, CancellationToken ct)
    {
        var status = ex switch
        {
            ArgumentException => 400,
            NotFoundException => 404,
            ConflictException => 409,
            OperationCanceledException => 503,
            _ => 500,
        };
        // Подробности — в лог, клиенту — общий текст: как writeError в Go.
        if (status == 500) log.LogError(ex, "request failed");
        http.Response.StatusCode = status;
        await http.Response.WriteAsJsonAsync(new { error = status == 500 ? "internal error" : ex.Message }, ct);
        return true;
    }
}

public class OutboxRelay : BackgroundService
{
    // stoppingToken отменяется при остановке хоста — после того, как Kestrel
    // перестал принимать запросы.
    protected override async Task ExecuteAsync(CancellationToken stoppingToken)
    {
        while (!stoppingToken.IsCancellationRequested)
        {
            try { await Task.Delay(TimeSpan.FromSeconds(1), stoppingToken); }
            catch (OperationCanceledException) { break; }
        }
    }
}

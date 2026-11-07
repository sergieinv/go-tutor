using Microsoft.AspNetCore.Http.Timeouts;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddRequestTimeouts(); // аналог middleware с context.WithTimeout
var app = builder.Build();
app.UseRequestTimeouts();

// CancellationToken в параметрах = HttpContext.RequestAborted = r.Context() в Go:
// отменяется, когда клиент разорвал соединение.
app.MapGet("/report", async (CancellationToken ct) =>
{
    await SlowQuery(TimeSpan.FromSeconds(10), ct); // бросит OperationCanceledException
    return Results.Ok("готово");
});

// Таймаут на запрос: middleware отменит RequestAborted через 50 мс, поймает
// OperationCanceledException и сам ответит 504. В Go хендлер видит ctx.Err()
// и пишет 504 сам. Под отладчиком таймауты не срабатывают.
app.MapGet("/search", async (CancellationToken ct) =>
{
    await SlowQuery(TimeSpan.FromSeconds(1), ct);
    return Results.Ok("найдено");
}).WithRequestTimeout(TimeSpan.FromMilliseconds(50));

// Значения запроса — HttpContext.Items (в Go — context.WithValue).
// Фоновая работа после ответа — без RequestAborted, как WithoutCancel.
app.MapPost("/orders", (HttpContext http) =>
{
    http.Items["RequestId"] = http.Request.Headers["X-Request-Id"].ToString();
    var id = (string)http.Items["RequestId"]!;
    // В фон — копию значения, не HttpContext: после ответа он переиспользуется.
    _ = Task.Run(() => Console.WriteLine($"аудит {id}")); // в реальном коде — очередь
    return Results.Ok("заказ принят");
});

app.Run();

static Task SlowQuery(TimeSpan d, CancellationToken ct) => Task.Delay(d, ct);

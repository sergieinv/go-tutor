using System.Net;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Server.Kestrel.Core;

var builder = WebApplication.CreateBuilder(args);

// KestrelServerLimits ≈ таймауты http.Server. У Kestrel они включены по умолчанию,
// у http.Server все нулевые — задавайте явно.
builder.WebHost.ConfigureKestrel(o =>
{
    o.Limits.RequestHeadersTimeout = TimeSpan.FromSeconds(2); // ≈ ReadHeaderTimeout (по умолчанию 30 с)
    o.Limits.KeepAliveTimeout = TimeSpan.FromSeconds(60);     // ≈ IdleTimeout (по умолчанию 130 с)
    o.Limits.MaxRequestHeadersTotalSize = 64 * 1024;          // ≈ MaxHeaderBytes
    o.Listen(IPAddress.Loopback, 0);                           // 127.0.0.1:0; ListenLocalhost(0) бросит исключение
});

var app = builder.Build();
var counter = new Counter();

// Минимальный API: хендлер возвращает IResult, фреймворк сам пишет статус и заголовки.
app.MapGet("/visits", () => Results.Text($"визит {counter.Next()}\n"));
app.MapPost("/items", () => Results.Created("/items/1", "создано"));

// Низкоуровневый вариант ближе к Go: HttpContext ≈ ResponseWriter + *Request.
app.Map("/raw", async (HttpContext ctx) =>
{
    ctx.Response.StatusCode = StatusCodes.Status201Created;
    await ctx.Response.WriteAsync("создано");
    // После старта ответа заголовки менять нельзя: здесь — исключение,
    // в Go — молча игнорируется.
    Console.WriteLine(ctx.Response.HasStarted); // True
});

app.Run(); // блокирует, как srv.Serve(ln)

class Counter
{
    private int _hits;
    // Запросы и здесь обрабатываются параллельно — нужна синхронизация.
    public int Next() => Interlocked.Increment(ref _hits);
}

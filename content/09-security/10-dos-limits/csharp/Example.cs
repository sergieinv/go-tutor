using System.Text.RegularExpressions;
using System.Threading.RateLimiting;
using Microsoft.AspNetCore.RateLimiting;

var builder = WebApplication.CreateBuilder(args);

// Kestrel ставит лимиты сам; здесь они лишь ужесточены.
// В Go у http.Server все таймауты нулевые — каждый пишете вы.
builder.WebHost.ConfigureKestrel(o =>
{
    o.Limits.RequestHeadersTimeout = TimeSpan.FromSeconds(5);   // ReadHeaderTimeout
    o.Limits.KeepAliveTimeout = TimeSpan.FromSeconds(60);       // IdleTimeout
    o.Limits.MaxRequestHeadersTotalSize = 16 * 1024;            // MaxHeaderBytes
    o.Limits.MaxRequestBodySize = 1024 * 1024;                  // MaxBytesReader
});

// Распаковка gzip-тела с тем же лимитом MaxRequestBodySize —
// защита от бомбы встроена. В Go распаковку и лимит пишете сами.
builder.Services.AddRequestDecompression();

// Лимит одновременных обработок: отказ сразу, без очереди, 503.
// В Go — буферизованный канал или semaphore.Weighted.TryAcquire.
builder.Services.AddRateLimiter(o =>
{
    o.RejectionStatusCode = StatusCodes.Status503ServiceUnavailable;
    o.AddConcurrencyLimiter("heavy", l => { l.PermitLimit = 8; l.QueueLimit = 0; });
});

var app = builder.Build();
app.UseRequestDecompression();
app.UseRateLimiter();

// [RequestSizeLimit] на action ≈ http.MaxBytesReader в хендлере.
app.MapPost("/upload", async (HttpRequest req) =>
{
    using var ms = new MemoryStream();
    // Сверх лимита Kestrel бросит BadHttpRequestException → 413;
    // распакованное тело сверх лимита — InvalidOperationException (→ 500).
    await req.Body.CopyToAsync(ms);
    return Results.Ok(ms.Length);
}).RequireRateLimiting("heavy");

// Backtracking-движок: без таймаута ^(a+)+$ уязвим к ReDoS.
// Go-regexp всегда линейный — как RegexOptions.NonBacktracking.
var safe = new Regex("^(a+)+$", RegexOptions.None, TimeSpan.FromMilliseconds(100));
app.MapGet("/match", (string s) => s.Length > 1000 ? Results.BadRequest() : Results.Ok(safe.IsMatch(s)));

app.Run();

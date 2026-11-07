using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Http;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddSingleton<RequestIdMiddleware>();
var app = builder.Build();

// Порядок Use = порядок вложенности: первый — внешний, как logging(recoverer(requestID(mux))).
app.Use(async (ctx, next) =>
{
    await next(ctx);
    // Статус доступен без обёртки ResponseWriter: HttpResponse хранит его сам.
    Console.WriteLine($"лог: {ctx.TraceIdentifier} {ctx.Request.Method} {ctx.Request.Path} → {ctx.Response.StatusCode}");
});

app.Use(async (ctx, next) =>
{
    try { await next(ctx); }
    catch (Exception e) when (!ctx.Response.HasStarted) // в Go — recover() в defer
    {
        Console.WriteLine($"recover: {e.Message}");
        ctx.Response.StatusCode = StatusCodes.Status500InternalServerError;
        await ctx.Response.WriteAsync("internal error");
    }
});

app.UseMiddleware<RequestIdMiddleware>(); // IMiddleware ≈ структура с полем next

app.MapGet("/orders/{id}", (string id, HttpContext ctx, CancellationToken ct) =>
{
    // ct = HttpContext.RequestAborted ≈ r.Context(): отменяется, когда клиент ушёл.
    if (id == "13") throw new InvalidOperationException("заказ 13 сломан");
    return $"заказ {id} ({ctx.Items["RequestId"]})";
});

app.Run();

class RequestIdMiddleware : IMiddleware
{
    private int _last;

    public Task InvokeAsync(HttpContext ctx, RequestDelegate next)
    {
        var id = $"req-{Interlocked.Increment(ref _last)}";
        ctx.Response.Headers["X-Request-ID"] = id;
        // Items — изменяемый словарь, общий для всех слоёв: внешние middleware
        // увидят значение. В Go context неизменяем, и наружу значение не поднимется.
        ctx.Items["RequestId"] = id;
        ctx.TraceIdentifier = id;
        return next(ctx);
    }
}

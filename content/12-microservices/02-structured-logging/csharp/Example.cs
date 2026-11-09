// ASP.NET Core: JSON-логи из коробки, атрибуты запроса — через scope.
// Serilog делает то же через LogContext.PushProperty и Enrich.FromLogContext.
// В Go: slog.NewJSONHandler + своя обёртка slog.Handler, которая берёт
// атрибуты из context.Context, — scope'ов и провайдеров нет.
var builder = WebApplication.CreateBuilder(args);
builder.Logging.ClearProviders();
builder.Logging.AddJsonConsole(o =>
{
    o.IncludeScopes = true; // scope попадает в каждую запись — как ctx-атрибуты
    o.TimestampFormat = null;
});
// Уровень — из appsettings.json ("Logging:LogLevel:Default") или кодом;
// в Go — HandlerOptions.Level и slog.LevelVar для смены на лету.
builder.Logging.SetMinimumLevel(LogLevel.Information);
// TraceId/SpanId/ParentId текущей Activity попадают в scope сами: хост
// включает ActivityTrackingOptions по умолчанию (без хоста — настроить
// LoggerFactoryOptions.ActivityTrackingOptions вручную). Аналог
// trace_id/span_id из trace.SpanContextFromContext(ctx) в Go.
var app = builder.Build();

// Middleware: request id из заголовка — в scope на время запроса.
app.Use(async (ctx, next) =>
{
    var log = ctx.RequestServices.GetRequiredService<ILogger<Program>>();
    var id = ctx.Request.Headers["X-Request-Id"].FirstOrDefault() ?? ctx.TraceIdentifier;
    using (log.BeginScope(new Dictionary<string, object> { ["request_id"] = id }))
    {
        await next(ctx);
    }
});

app.MapPost("/orders", (ILogger<Program> log) =>
{
    // Шаблон сообщения одновременно и текст, и источник свойств.
    // В slog сообщение константное, данные — отдельными атрибутами.
    log.LogInformation("create order {Customer} {Qty}", "alice", 2);
    return Results.Created("/orders/o-1", new { id = "o-1" });
});

app.Run();

// Логгер приходит через DI с категорией ILogger<T>. В Go логгер передают явно:
// полем структуры или параметром конструктора, без контейнера.

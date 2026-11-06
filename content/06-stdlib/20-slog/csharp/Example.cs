// NuGet: Microsoft.Extensions.Logging.Console
using System.Text.Encodings.Web;
using Microsoft.Extensions.Logging;

// Провайдер (консоль в JSON) ≈ slog.NewJSONHandler; минимальный уровень ≈ HandlerOptions.Level
using var factory = LoggerFactory.Create(b => b
    .AddJsonConsole(o =>
    {
        o.IncludeScopes = true; // без этого свойства BeginScope в вывод не попадут
        o.JsonWriterOptions = new() { Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }; // кириллица без \uXXXX
    })
    .SetMinimumLevel(LogLevel.Information));
ILogger log = factory.CreateLogger("Shop"); // категория; в slog её роль играет атрибут или группа

// Шаблон сообщения — одновременно текст и источник свойств.
// В slog сообщение — константа, данные — отдельные атрибуты:
//   log.Info("сервер запущен", "port", 8080)
log.LogInformation("Сервер запущен на порту {Port}", 8080);
log.LogDebug("не выведется: уровень ниже Information");

// BeginScope ≈ logger.With(...): свойства для группы записей.
// Но scope живёт до Dispose, а With возвращает новый логгер.
using (log.BeginScope(new Dictionary<string, object> { ["RequestId"] = "r-42" }))
{
    log.LogWarning("Медленный запрос: {Ms} мс", 1250);
}

// Прямого аналога LogValuer нет: чтобы скрыть пароль, переопределяют ToString,
// используют [LoggerMessage] + [LogProperties] с [LogPropertyIgnore] или редакцией
// (.NET 8+, Microsoft.Extensions.Telemetry) либо деструктуризацию Serilog (@).
log.LogError("Вход не удался для {User}", new User(7, "a@b.c", "secret"));

record User(int Id, string Email, string Password)
{
    public override string ToString() => $"{{ Id = {Id}, Email = {Email} }}";
}

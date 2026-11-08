using Microsoft.AspNetCore.Diagnostics;

var builder = WebApplication.CreateBuilder(args);

// Секреты — из конфигурации (переменные окружения, User Secrets, Key Vault),
// а не из кода. В Go — os.LookupEnv и чтение файла NAME_FILE.
var dbPassword = new Secret(builder.Configuration["DB_PASSWORD"]
    ?? throw new InvalidOperationException("секрет DB_PASSWORD не задан"));

builder.Services.AddProblemDetails();
var app = builder.Build();

// Одна точка, где ошибка делится на «в лог» и «клиенту».
// В Go — обёртка Handle над хендлерами, возвращающими error.
app.UseExceptionHandler(errApp => errApp.Run(async ctx =>
{
    var ex = ctx.Features.Get<IExceptionHandlerFeature>()?.Error;
    var id = ctx.TraceIdentifier;
    app.Logger.LogError(ex, "request failed {RequestId}", id); // подробности — в лог
    ctx.Response.StatusCode = StatusCodes.Status500InternalServerError;
    await ctx.Response.WriteAsJsonAsync(new { error = "internal error", request_id = id });
}));

app.MapGet("/orders/{id}", (int id) =>
{
    // Исключение уйдёт в UseExceptionHandler; текст клиенту не попадёт.
    // В Go утечка — это http.Error(w, err.Error(), 500), написанный руками.
    throw new InvalidOperationException("pq: password authentication failed for user \"app\"");
});

app.Logger.LogInformation("config loaded: {Db}", dbPassword); // напечатает [REDACTED]
app.Run();

// Аналог Go-типа Secret: ToString и сериализация скрывают значение.
// Record здесь был бы ловушкой: его ToString печатает все свойства.
[System.Text.Json.Serialization.JsonConverter(typeof(SecretConverter))]
public sealed class Secret(string value)
{
    public string Reveal() => value;
    public override string ToString() => "[REDACTED]";
}

public sealed class SecretConverter : System.Text.Json.Serialization.JsonConverter<Secret>
{
    public override Secret Read(ref System.Text.Json.Utf8JsonReader r, Type t, System.Text.Json.JsonSerializerOptions o)
        => new(r.GetString() ?? "");

    public override void Write(System.Text.Json.Utf8JsonWriter w, Secret s, System.Text.Json.JsonSerializerOptions o)
        => w.WriteStringValue("[REDACTED]");
}

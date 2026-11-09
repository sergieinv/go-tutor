// ASP.NET Core (Microsoft.NET.Sdk.Web): health checks с тегами, пробы
// и остановка через IHostApplicationLifetime. Образ — chiseled:
//
//   FROM mcr.microsoft.com/dotnet/sdk:10.0 AS build
//   RUN dotnet publish -c Release -o /out
//   FROM mcr.microsoft.com/dotnet/aspnet:10.0-noble-chiseled   # без shell, nonroot
//   COPY --from=build /out /app
//   ENTRYPOINT ["dotnet", "/app/Orders.dll"]
using Microsoft.AspNetCore.Diagnostics.HealthChecks;
using Microsoft.Extensions.Diagnostics.HealthChecks;

var builder = WebApplication.CreateBuilder(args);
builder.Services.Configure<HostOptions>(o => o.ShutdownTimeout = TimeSpan.FromSeconds(20));
builder.Services.AddSingleton<Lifecycle>();
builder.Services.AddHealthChecks()
    // В проде: .AddNpgSql(connectionString, tags: ["ready"]) из AspNetCore.HealthChecks.NpgSql.
    .AddCheck<DbCheck>("db", tags: ["ready"])
    .AddCheck<StageCheck>("stage", tags: ["ready"]);

var app = builder.Build();
var life = app.Services.GetRequiredService<Lifecycle>();

// Liveness — без проверок: Predicate = _ => false значит «процесс отвечает».
app.MapHealthChecks("/livez", new HealthCheckOptions { Predicate = _ => false });
app.MapHealthChecks("/readyz", new HealthCheckOptions { Predicate = c => c.Tags.Contains("ready") });
app.MapGet("/startupz", () => life.Started ? Results.Ok("ok") : Results.StatusCode(503));

// SIGTERM → ApplicationStopping: сначала draining и пауза для балансировщиков,
// потом хост сам дождётся запросов (ShutdownTimeout). В Go — тот же порядок,
// но явным кодом в main после <-ctx.Done().
app.Lifetime.ApplicationStopping.Register(() =>
{
    life.Draining = true;
    Thread.Sleep(TimeSpan.FromSeconds(5));
});

life.Started = true; // миграции применены, кеш прогрет
app.Run();

public sealed class Lifecycle
{
    public volatile bool Started;
    public volatile bool Draining;
}

public sealed class StageCheck(Lifecycle life) : IHealthCheck
{
    public Task<HealthCheckResult> CheckHealthAsync(HealthCheckContext context, CancellationToken ct = default) =>
        Task.FromResult(life.Started && !life.Draining
            ? HealthCheckResult.Healthy()
            : HealthCheckResult.Unhealthy(life.Draining ? "draining" : "starting"));
}

public sealed class DbCheck : IHealthCheck
{
    public async Task<HealthCheckResult> CheckHealthAsync(HealthCheckContext context, CancellationToken ct = default)
    {
        await Task.Delay(1, ct); // здесь был бы SELECT 1 с таймаутом
        return HealthCheckResult.Healthy();
    }
}

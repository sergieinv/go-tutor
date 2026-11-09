using Microsoft.Extensions.Diagnostics.HealthChecks;

// Grpc.AspNetCore.HealthChecks связывает стандартные health checks ASP.NET Core
// (IHealthCheck, AddCheck) с сервисом grpc.health.v1.Health. В Go
// health.Server — просто хранилище статусов: кто и когда их обновляет,
// решаете вы (см. задание урока).
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddGrpc();
builder.Services
    .AddGrpcHealthChecks(o =>
    {
        // Статус сервиса = результат проверок с тегом "db".
        o.Services.Map("shop.v1.OrderService", r => r.Tags.Contains("db"));
    })
    .AddCheck<PostgresCheck>("postgres", tags: ["db"]);
// Reflection — пакет Grpc.AspNetCore.Server.Reflection.
builder.Services.AddGrpcReflection();

var app = builder.Build();
// app.MapGrpcService<OrderService>(); — класс, сгенерированный Grpc.Tools из .proto
app.MapGrpcHealthChecksService();
if (app.Environment.IsDevelopment())
{
    app.MapGrpcReflectionService(); // в проде — выключено, как и Swagger UI
}
app.Run();

// Check по умолчанию заново выполняет проверки на каждый вызов (UseHealthChecksCache = false),
// а Watch получает результаты фонового HealthCheckPublisher (по умолчанию раз в 30 с).
// При остановке приложения Watch завершаются статусом NotServing — как hs.Shutdown() в Go.
public class PostgresCheck : IHealthCheck
{
    public async Task<HealthCheckResult> CheckHealthAsync(HealthCheckContext context, CancellationToken ct = default)
    {
        await Task.Delay(1, ct); // здесь был бы SELECT 1 с коротким таймаутом
        return HealthCheckResult.Healthy();
    }
}

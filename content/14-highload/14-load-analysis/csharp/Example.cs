// Те же три приёма «Заказов» из labs в ASP.NET Core: кеш каталога,
// ограничитель CreateOrder с 429 и история с реплики по токену LSN.
// В Go каждый — свой пакет на сотню-две строк; здесь — готовые блоки.
//
// HybridCache регистрируют пакетом Microsoft.Extensions.Caching.Hybrid:
//   builder.Services.AddHybridCache();
// (сам абстрактный класс — в Microsoft.Extensions.Caching.Abstractions).
// Нагрузку дают NBomber (сценарии на C#) или k6 — как loadgen в labs.
using System.Threading.RateLimiting;
using Microsoft.AspNetCore.RateLimiting;
using Microsoft.Extensions.Caching.Hybrid;
using Npgsql;
using NpgsqlTypes;

public sealed record Product(long Id, string Name, long PriceCents);

// Обёртка, чтобы «товара нет» тоже попадало в кеш (отрицательный кеш).
public sealed record CatalogEntry(Product? Product);

public static class Program
{
    public static void Main(string[] args)
    {
        var builder = WebApplication.CreateBuilder(args);

        // Один источник на основной и реплику: Npgsql сам выбирает сервер
        // по Target Session Attributes. Задержку реплики он не учитывает.
        var multi = new NpgsqlDataSourceBuilder(
            builder.Configuration["Orders:Hosts"] ?? "Host=postgres,postgres-replica;Database=orders").BuildMultiHost();
        builder.Services.AddSingleton(multi);

        // Load shedding: 16 одновременно, 16 в очереди, остальным — 429.
        // По умолчанию отказ — 503; Retry-After ставим сами.
        builder.Services.AddRateLimiter(o =>
        {
            o.RejectionStatusCode = StatusCodes.Status429TooManyRequests;
            o.OnRejected = (ctx, _) =>
            {
                ctx.HttpContext.Response.Headers.RetryAfter = "1";
                return ValueTask.CompletedTask;
            };
            o.AddConcurrencyLimiter("create", c =>
            {
                c.PermitLimit = 16;
                c.QueueLimit = 16;
                c.QueueProcessingOrder = QueueProcessingOrder.OldestFirst;
            });
        });

        var app = builder.Build();
        app.UseRateLimiter();

        // Cache-aside со встроенной защитой от stampede: одновременные
        // промахи по ключу ждут одну фабрику (в Go — singleflight).
        // Параметры записи задаются до вызова фабрики, поэтому короткий TTL
        // только для «нет такого», как в labs, так не выразить.
        app.MapGet("/v1/products/{id:long}", async (long id, HybridCache cache, NpgsqlMultiHostDataSource db, CancellationToken ct) =>
        {
            var entry = await cache.GetOrCreateAsync($"orders:catalog:v1:{id}",
                async t => new CatalogEntry(await LoadProduct(db, id, t)),
                new HybridCacheEntryOptions { Expiration = TimeSpan.FromMinutes(5) },
                cancellationToken: ct);
            return entry.Product is { } p ? Results.Ok(p) : Results.NotFound();
        });

        app.MapPost("/v1/orders", async (NpgsqlMultiHostDataSource db, CancellationToken ct) =>
        {
            var primary = db.WithTargetSession(TargetSessionAttributes.Primary);
            // ... заказ записан; токен — позиция WAL после COMMIT.
            await using var cmd = primary.CreateCommand("SELECT pg_current_wal_lsn()");
            var lsn = (NpgsqlLogSequenceNumber)(await cmd.ExecuteScalarAsync(ct))!;
            return Results.Ok(new { consistency_token = $"lsn:{lsn}" });
        }).RequireRateLimiting("create");

        // История: реплика, если она проиграла токен, иначе основной.
        app.MapGet("/v1/orders", async (long customer_id, string? consistency_token, NpgsqlMultiHostDataSource db, CancellationToken ct) =>
        {
            var source = db.WithTargetSession(TargetSessionAttributes.PreferStandby);
            if (consistency_token is { } tok && tok.StartsWith("lsn:")
                && NpgsqlLogSequenceNumber.TryParse(tok.AsSpan(4), out var need))
            {
                await using var replay = source.CreateCommand("SELECT pg_last_wal_replay_lsn()");
                var got = await replay.ExecuteScalarAsync(ct);
                if (got is not NpgsqlLogSequenceNumber r || r < need)
                    source = db.WithTargetSession(TargetSessionAttributes.Primary);
            }
            await using var q = source.CreateCommand("SELECT count(*) FROM orders WHERE customer_id = $1");
            q.Parameters.AddWithValue(customer_id);
            return Results.Ok(new { orders = await q.ExecuteScalarAsync(ct) });
        });

        app.Run();
    }

    static async Task<Product?> LoadProduct(NpgsqlMultiHostDataSource db, long id, CancellationToken ct)
    {
        await using var cmd = db.CreateCommand("SELECT id, name, price_cents FROM products WHERE id = $1");
        cmd.Parameters.AddWithValue(id);
        await using var rd = await cmd.ExecuteReaderAsync(ct);
        return await rd.ReadAsync(ct) ? new Product(rd.GetInt64(0), rd.GetString(1), rd.GetInt64(2)) : null;
    }
}

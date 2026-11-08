using System.Data.Common;

// ASP.NET Core: граф зависимостей собирает DI-контейнер по регистрациям,
// конфигурацию — IConfiguration, жизненный цикл — Generic Host.
// В Go всё это — функция run(): открыть базу, создать хранилище и сервис,
// собрать сервер с таймаутами, запустить, дождаться сигнала и остановить.
var builder = WebApplication.CreateBuilder(args);

// Провайдер даёт DbDataSource с пулом: NpgsqlDataSource.Create(cs) или
// new MySqlDataSource(cs). Здесь заглушка, чтобы файл собирался без NuGet.
builder.Services.AddSingleton<DbDataSource>(_ => throw new NotSupportedException("подключите провайдер"));
builder.Services.AddScoped<OrderStore>();
builder.Services.AddProblemDetails();
builder.WebHost.ConfigureKestrel(k =>
{
    k.Limits.MaxRequestBodySize = 64 * 1024;                     // http.MaxBytesReader
    k.Limits.RequestHeadersTimeout = TimeSpan.FromSeconds(2);    // ReadHeaderTimeout
});

var app = builder.Build();
app.UseExceptionHandler(); // 500 без подробностей наружу — в Go это fail()

app.MapPost("/orders", async (CreateOrder req, OrderStore store, CancellationToken ct) =>
{
    // CancellationToken запроса — аналог r.Context(): клиент ушёл — запрос к базе отменяется.
    try
    {
        var id = await store.CreateAsync(req.Items, ct);
        return Results.Created($"/orders/{id}", new { id, status = "reserved", req.Items });
    }
    catch (OutOfStockException)
    {
        return Results.Conflict(new { error = "out of stock" }); // errors.Is(err, ErrOutOfStock)
    }
});

app.Run();

public record ItemDto(long ProductId, int Quantity);
public record CreateOrder(ItemDto[] Items);
public class OutOfStockException : Exception;

public class OrderStore(DbDataSource db)
{
    public async Task<long> CreateAsync(ItemDto[] items, CancellationToken ct)
    {
        // TransactionScope не используем: транзакция явная, как *sql.Tx в Go.
        await using var conn = await db.OpenConnectionAsync(ct);
        await using var tx = await conn.BeginTransactionAsync(ct); // Dispose без Commit = Rollback

        var id = Convert.ToInt64(await Cmd(conn, tx, "INSERT INTO orders (status) VALUES ('reserved') RETURNING id")
            .ExecuteScalarAsync(ct));
        foreach (var it in items)
        {
            // Атомарный резерв: проверка остатка и списание — один UPDATE.
            var n = await Cmd(conn, tx, "UPDATE products SET available = available - @q WHERE id = @id AND available >= @q",
                ("@q", it.Quantity), ("@id", it.ProductId)).ExecuteNonQueryAsync(ct);
            if (n == 0) throw new OutOfStockException();
        }
        await Cmd(conn, tx, "INSERT INTO outbox (type, payload) VALUES ('order.created', @p)",
            ("@p", $"{{\"order_id\":{id}}}")).ExecuteNonQueryAsync(ct);
        await tx.CommitAsync(ct);
        return id;
    }

    // Значения — только параметрами, как плейсхолдеры в Go. Синтаксис имени
    // параметра (@q, $1) зависит от провайдера.
    static DbCommand Cmd(DbConnection c, DbTransaction tx, string sql, params (string Name, object Value)[] ps)
    {
        var cmd = c.CreateCommand();
        cmd.Transaction = tx;
        cmd.CommandText = sql;
        foreach (var (name, value) in ps)
        {
            var p = cmd.CreateParameter();
            p.ParameterName = name;
            p.Value = value;
            cmd.Parameters.Add(p);
        }
        return cmd;
    }
}

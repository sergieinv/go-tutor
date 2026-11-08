using Npgsql;

// Npgsql (NuGet-пакет Npgsql) — то же место, что pgx: родной драйвер PostgreSQL.
// NpgsqlDataSource — аналог pgxpool.Pool: пул внутри, создаётся один раз.
var connString = Environment.GetEnvironmentVariable("POSTGRES_DSN_NPGSQL");
if (connString is null)
{
    Console.WriteLine("нужна строка подключения Npgsql");
    return;
}
var builder = new NpgsqlDataSourceBuilder(connString);
builder.EnableDynamicJson(); // POCO <-> jsonb через System.Text.Json; в pgx — по умолчанию
await using var db = builder.Build();

// Плейсхолдеры $1, $2 — позиционные параметры без имён, как в pgx.
// RETURNING — тот же SQL, ExecuteScalar вместо Scan.
await using (var cmd = db.CreateCommand(
    "INSERT INTO products (sku, price, attrs, tags) VALUES ($1, $2, $3, $4) RETURNING id"))
{
    cmd.Parameters.Add(new() { Value = "mug" });
    cmd.Parameters.Add(new() { Value = 450L });
    cmd.Parameters.Add(new() { Value = new Attrs("red", 300), NpgsqlDbType = NpgsqlTypes.NpgsqlDbType.Jsonb });
    cmd.Parameters.Add(new() { Value = new[] { "kitchen", "gift" } }); // string[] -> text[]
    var id = (long)(await cmd.ExecuteScalarAsync())!;
    Console.WriteLine($"вставлен id {id}");
}

// Ошибка сервера — PostgresException с SqlState; в Go — *pgconn.PgError и Code.
try
{
    await using var dup = db.CreateCommand("INSERT INTO products (sku, price) VALUES ('mug', 500)");
    await dup.ExecuteNonQueryAsync();
}
catch (PostgresException ex) when (ex.SqlState == PostgresErrorCodes.UniqueViolation)
{
    Console.WriteLine($"ошибка: {ex.SqlState} {ex.ConstraintName}");
}

// NpgsqlBatch — аналог pgx.Batch: несколько команд за один сетевой обмен.
await using (var batch = db.CreateBatch())
{
    foreach (var (sku, price) in new[] { ("plate", 100L), ("bowl", 200L) })
    {
        var bc = new NpgsqlBatchCommand("INSERT INTO products (sku, price) VALUES ($1, $2)");
        bc.Parameters.Add(new() { Value = sku });
        bc.Parameters.Add(new() { Value = price });
        batch.BatchCommands.Add(bc);
    }
    await batch.ExecuteNonQueryAsync();
}

// COPY: BeginBinaryImport ↔ pool.CopyFrom. Соединение берём явно.
await using (var conn = await db.OpenConnectionAsync())
{
    await using var writer = await conn.BeginBinaryImportAsync(
        "COPY products (sku, price) FROM STDIN (FORMAT BINARY)");
    foreach (var (sku, price) in new[] { ("spoon", 50L), ("knife", 70L) })
    {
        await writer.StartRowAsync();
        await writer.WriteAsync(sku);
        await writer.WriteAsync(price);
    }
    Console.WriteLine($"COPY строк: {await writer.CompleteAsync()}");
}

// LISTEN тоже живёт на соединении: событие Notification + WaitAsync,
// в Go — Acquire и WaitForNotification.
await using (var listener = await db.OpenConnectionAsync())
{
    listener.Notification += (_, e) => Console.WriteLine($"уведомление: {e.Channel} {e.Payload}");
    await using (var listen = new NpgsqlCommand("LISTEN product_events", listener))
        await listen.ExecuteNonQueryAsync();
    await using (var notify = db.CreateCommand("SELECT pg_notify('product_events', $1)"))
    {
        notify.Parameters.Add(new() { Value = "price changed: mug" });
        await notify.ExecuteNonQueryAsync();
    }
    using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
    await listener.WaitAsync(cts.Token);
}

public record Attrs(string Color, int Weight);

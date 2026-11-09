using Npgsql;

// Те же приёмы в .NET: пул и таймауты живут в строке подключения Npgsql,
// а не в коде, и у пула есть лимит по умолчанию (Max Pool Size = 100).
var connString = Environment.GetEnvironmentVariable("POSTGRES_DSN_NPGSQL");
if (connString is null)
{
    Console.WriteLine("нужна строка подключения Npgsql");
    return;
}

var csb = new NpgsqlConnectionStringBuilder(connString)
{
    MaxPoolSize = 20,         // ≈ pgxpool MaxConns
    Timeout = 5,              // сек. ожидания соединения (в Go — дедлайн context)
    CommandTimeout = 2,       // сек. на команду; по умолчанию 30
    CancellationTimeout = 2000, // мс ждать ответа на отмену, потом рвать соединение
};
await using var db = NpgsqlDataSource.Create(csb.ConnectionString);

// Keyset-пагинация: то же сравнение кортежей, что в Go.
// Курсор — (created_at, id) последней строки предыдущей страницы.
var page = await ListOrders(db, customerId: 42, after: null, limit: 20);
Console.WriteLine($"заказов на странице: {page.Count}");

static async Task<List<(long Id, DateTime CreatedAt)>> ListOrders(
    NpgsqlDataSource db, long customerId, (DateTime At, long Id)? after, int limit,
    CancellationToken ct = default)
{
    var sql = "SELECT id, created_at FROM orders WHERE customer_id = $1";
    if (after is not null)
        sql += " AND (created_at, id) < ($2, $3)";
    sql += " ORDER BY created_at DESC, id DESC LIMIT " + (after is null ? "$2" : "$4");

    await using var cmd = db.CreateCommand(sql);
    cmd.Parameters.Add(new() { Value = customerId });
    if (after is { } a)
    {
        cmd.Parameters.Add(new() { Value = a.At });
        cmd.Parameters.Add(new() { Value = a.Id });
    }
    cmd.Parameters.Add(new() { Value = limit });

    // Отмена токена — как отмена context: Npgsql шлёт серверу запрос отмены.
    var result = new List<(long, DateTime)>();
    await using var reader = await cmd.ExecuteReaderAsync(ct);
    while (await reader.ReadAsync(ct))
        result.Add((reader.GetInt64(0), reader.GetDateTime(1)));
    return result;
}

// EF Core: N+1 появляется из ленивой загрузки (order.Customer.Name в цикле).
// Лечение — Include(o => o.Customer) или проекция Select(...) в один запрос;
// в Go запросы видны в коде, и их число проверяют тестом или QueryTracer.

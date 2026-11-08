using System.Text.Json;
using Npgsql;

// Индексы и планы — свойства PostgreSQL, а не драйвера: SQL тот же.
// Разница в том, как драйвер готовит запросы.
var connString = Environment.GetEnvironmentVariable("POSTGRES_DSN_NPGSQL");
if (connString is null)
{
    Console.WriteLine("нужна строка подключения Npgsql");
    return;
}

// Npgsql по умолчанию НЕ готовит запросы на сервере: только явный cmd.Prepare()
// или автоподготовка «Max Auto Prepare=N» в строке подключения. pgx наоборот —
// QueryExecModeCacheStatement по умолчанию готовит и кеширует всё.
// За PgBouncer в режиме transaction в Npgsql обычно добавляют «No Reset On Close=true»
// и не включают автоподготовку (или берут PgBouncer 1.21+ с max_prepared_statements).
var csb = new NpgsqlConnectionStringBuilder(connString) { MaxAutoPrepare = 20, AutoPrepareMinUsages = 2 };
await using var db = NpgsqlDataSource.Create(csb.ConnectionString);

await using (var create = db.CreateCommand(
    "CREATE INDEX IF NOT EXISTS orders_customer_created ON orders (customer_id, created_at)"))
    await create.ExecuteNonQueryAsync();

// EXPLAIN (ANALYZE, FORMAT JSON) возвращает одну строку с JSON — как в Go.
await using var cmd = db.CreateCommand(
    "EXPLAIN (ANALYZE, FORMAT JSON) SELECT id FROM orders WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 5");
cmd.Parameters.Add(new() { Value = 42 });
var json = (string)(await cmd.ExecuteScalarAsync())!;

using var doc = JsonDocument.Parse(json);
Print(doc.RootElement[0].GetProperty("Plan"), 0);

// Тот же обход дерева, что printNode в Go. "Actual Rows" в PostgreSQL 18 дробное —
// GetDouble, а не GetInt32.
static void Print(JsonElement node, int depth)
{
    var line = new string(' ', depth * 2) + node.GetProperty("Node Type").GetString();
    if (node.TryGetProperty("Index Name", out var index))
        line += " using " + index.GetString();
    line += $" (строк: {node.GetProperty("Actual Rows").GetDouble():0})";
    Console.WriteLine(line);
    if (node.TryGetProperty("Plans", out var children))
        foreach (var child in children.EnumerateArray())
            Print(child, depth + 1);
}

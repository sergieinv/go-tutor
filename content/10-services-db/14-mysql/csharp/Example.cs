using MySqlConnector; // NuGet: MySqlConnector

// Строка подключения — через билдер, как mysql.Config в Go: пароль со
// служебными символами не сломает её. DATETIME MySqlConnector читает сам
// (в Go для этого нужен parseTime), соединение всегда в utf8mb4.
var csb = new MySqlConnectionStringBuilder
{
    Server = "127.0.0.1",
    Port = 3306,
    UserID = "app",
    Password = "p;ss=word",
    Database = "shop",
    ConnectionTimeout = 5,       // секунды; в Go — cfg.Timeout
    DefaultCommandTimeout = 30,  // таймаут команды; в Go — context запроса
    DateTimeKind = MySqlDateTimeKind.Utc,
    // По умолчанию RowsAffected здесь — найденные строки: upsert без
    // изменений вернёт 1. Go-драйвер считает изменённые — вернёт 0.
    UseAffectedRows = true,
};

// MySqlDataSource — пул, как *sql.DB. Создаётся один раз на приложение.
await using var db = new MySqlDataSourceBuilder(csb.ConnectionString).Build();
if (args.Length > 0)
{
    var id = await new Orders(db).CreateAsync("кофе ☕", CancellationToken.None);
    Console.WriteLine(id);
}

public class Orders(MySqlDataSource db)
{
    public async Task<long> CreateAsync(string title, CancellationToken ct)
    {
        // Повтор транзакции целиком — вручную или политикой Polly; в EF Core —
        // EnableRetryOnFailure. В Go — свой цикл вокруг BeginTx.
        for (var attempt = 1; ; attempt++)
        {
            await using var conn = await db.OpenConnectionAsync(ct);
            await using var tx = await conn.BeginTransactionAsync(ct);
            try
            {
                var cmd = new MySqlCommand("INSERT INTO orders (title, created) VALUES (@t, UTC_TIMESTAMP())", conn, tx);
                cmd.Parameters.AddWithValue("@t", title);
                await cmd.ExecuteNonQueryAsync(ct);
                var id = cmd.LastInsertedId; // res.LastInsertId() в Go

                var stock = new MySqlCommand(
                    "INSERT INTO stock (sku, qty) VALUES ('cup', 1) AS n ON DUPLICATE KEY UPDATE qty = stock.qty + n.qty",
                    conn, tx);
                await stock.ExecuteNonQueryAsync(ct);

                await tx.CommitAsync(ct);
                return id;
            }
            // MySqlException.ErrorCode ↔ (*mysql.MySQLError).Number в Go:
            // LockDeadlock = 1213, LockWaitTimeout = 1205, DuplicateKeyEntry = 1062.
            // IsTransient == true как раз для дедлока и истёкшего ожидания.
            catch (MySqlException e) when (e.IsTransient && attempt < 3)
            {
                // await using откатит транзакцию; следующая итерация — заново.
            }
        }
    }
}

using System.Data.Common;

// В ADO.NET пул прячется в провайдере и настраивается строкой подключения:
//   Npgsql:    "Host=db;Database=shop;Maximum Pool Size=20;Minimum Pool Size=0;
//               Timeout=15;Connection Idle Lifetime=300;Connection Lifetime=1800"
//   SqlClient: "Server=db;Database=shop;Max Pool Size=20;Connect Timeout=15"
// Go: те же ручки — методы *sql.DB (SetMaxOpenConns, SetConnMaxIdleTime,
// SetConnMaxLifetime), а ожидание соединения ограничивает context запроса.
// Источник данных создаёт провайдер (NpgsqlDataSource.Create(connString));
// здесь провайдера нет, чтобы код собирался без NuGet.
DbDataSource? dataSource = null;
if (dataSource is not null)
{
    var repo = new ItemRepo(dataSource);
    using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(2)); // дедлайн запроса
    Console.WriteLine(await repo.FirstNameAsync("drink", cts.Token));
}

public class ItemRepo(DbDataSource db)
{
    public async Task<string?> FirstNameAsync(string tag, CancellationToken ct)
    {
        // Открыть = взять из пула. Пул исчерпан — ждём до Timeout из строки
        // подключения, затем исключение «The connection pool has been exhausted».
        // В Go ожидание ограничивает ctx: QueryContext вернёт context.DeadlineExceeded.
        await using var conn = await db.OpenConnectionAsync(ct);
        await using var cmd = conn.CreateCommand();
        cmd.CommandText = "SELECT name FROM items WHERE tag = @tag ORDER BY id";
        var p = cmd.CreateParameter();
        p.ParameterName = "@tag";
        p.Value = tag;
        cmd.Parameters.Add(p);

        await using var reader = await cmd.ExecuteReaderAsync(ct);
        // Ранний return безопасен: await using закроет reader и вернёт соединение
        // в пул — как defer rows.Close() в Go. Без using соединение «утекает» так же.
        if (await reader.ReadAsync(ct))
            return reader.GetString(0); // остальные строки не дочитываем
        return null;
    }
}

// Метрики пула: в Go — db.Stats() (InUse, Idle, WaitCount, WaitDuration);
// в .NET — счётчики провайдера (Npgsql публикует их через EventCounters/Metrics).

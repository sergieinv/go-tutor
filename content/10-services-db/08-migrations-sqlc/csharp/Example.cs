using System.Data.Common;

// В EF Core то же самое выглядит так (нужен NuGet, поэтому в комментарии):
//   dotnet ef migrations add AddStock      // C#-миграция из диффа модели и снимка
//   await db.Database.MigrateAsync(ct);    // применить; журнал — __EFMigrationsHistory
//   var inStock = await db.Products.Where(p => p.Stock >= 1).OrderBy(p => p.Id).ToListAsync(ct);
// Ниже — как устроены goose и sqlc, на голом ADO.NET (по духу это DbUp + Dapper).

DbDataSource? source = null; // NpgsqlDataSource.Create(...) в настоящем приложении
if (source is not null)
{
    await Migrator.UpAsync(source, Migrator.Scripts, CancellationToken.None);
    var q = new Queries(source);
    foreach (var p in await q.ListInStockAsync(1, CancellationToken.None))
        Console.WriteLine(p);
}

public static class Migrator
{
    // Версия → SQL раздела Up; в goose это файлы 00001_*.sql, встроенные через go:embed.
    public static readonly SortedDictionary<long, string> Scripts = new()
    {
        [1] = "CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, price INTEGER NOT NULL)",
        [2] = "ALTER TABLE products ADD COLUMN stock INTEGER NOT NULL DEFAULT 0",
    };

    public static async Task UpAsync(DbDataSource db, SortedDictionary<long, string> scripts, CancellationToken ct)
    {
        await using var conn = await db.OpenConnectionAsync(ct);
        await Exec(conn, null, "CREATE TABLE IF NOT EXISTS db_version (version INTEGER PRIMARY KEY)", ct);
        await using var cmd = conn.CreateCommand();
        cmd.CommandText = "SELECT COALESCE(MAX(version), 0) FROM db_version";
        var current = Convert.ToInt64(await cmd.ExecuteScalarAsync(ct));

        foreach (var (version, sql) in scripts.Where(s => s.Key > current))
        {
            // Как в goose: каждая миграция и запись версии — в одной транзакции.
            await using var tx = await conn.BeginTransactionAsync(ct);
            await Exec(conn, tx, sql, ct);
            await Exec(conn, tx, "INSERT INTO db_version (version) VALUES (@v)", ct, version);
            await tx.CommitAsync(ct);
        }
    }

    static async Task Exec(DbConnection conn, DbTransaction? tx, string sql, CancellationToken ct, long? version = null)
    {
        await using var cmd = conn.CreateCommand();
        cmd.Transaction = tx;
        cmd.CommandText = sql;
        if (version is not null)
        {
            var p = cmd.CreateParameter(); // значения — только параметрами, даже свои
            p.ParameterName = "@v";
            p.Value = version.Value;
            cmd.Parameters.Add(p);
        }
        await cmd.ExecuteNonQueryAsync(ct);
    }
}

// То, что sqlc генерирует из «-- name: ListInStock :many»: тип строки и метод.
// В C# такой код пишут руками (или с Dapper), и ошибка в имени столбца видна только в рантайме.
public record ListInStockRow(long Id, string Name, long Stock);

public class Queries(DbDataSource db)
{
    public async Task<List<ListInStockRow>> ListInStockAsync(long minStock, CancellationToken ct)
    {
        await using var cmd = db.CreateCommand("SELECT id, name, stock FROM products WHERE stock >= @min ORDER BY id");
        var p = cmd.CreateParameter();
        p.ParameterName = "@min";
        p.Value = minStock;
        cmd.Parameters.Add(p);

        var items = new List<ListInStockRow>();
        await using var r = await cmd.ExecuteReaderAsync(ct);
        while (await r.ReadAsync(ct))
            items.Add(new ListInStockRow(r.GetInt64(0), r.GetString(1), r.GetInt64(2)));
        return items;
    }
}

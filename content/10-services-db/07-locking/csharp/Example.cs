using System.ComponentModel.DataAnnotations;
using System.Data;
using System.Data.Common;

// EF Core: оптимистичная блокировка объявляется на сущности. SaveChanges
// добавит в UPDATE условие WHERE ... AND version = @original и, если строк
// изменилось 0, бросит DbUpdateConcurrencyException. В Go то же условие
// пишут в SQL руками и проверяют RowsAffected.
public class Stock
{
    public long Id { get; set; }
    public long Qty { get; set; }

    [ConcurrencyCheck] // или [Timestamp] на byte[] RowVersion в SQL Server
    public long Version { get; set; }
}

// Без EF — как в Go: условие на версию в SQL и повтор всей операции.
// Провайдера нет, чтобы код собирался без NuGet; источник — NpgsqlDataSource.
public class StockRepo(DbDataSource db)
{
    public async Task ReserveAsync(long id, long n, CancellationToken ct)
    {
        // Повтор — обычно Polly (ResiliencePipeline с Retry и jitter);
        // в Go — свой цикл на десяток строк, как в задании урока.
        for (var attempt = 1; ; attempt++)
        {
            var (qty, version) = await GetAsync(id, ct);
            if (qty < n) throw new InvalidOperationException("недостаточно");
            if (await SaveAsync(id, qty - n, version, ct)) return;
            if (attempt == 5) throw new DBConcurrencyException("конфликт версий");
            var cap = Math.Min(10 << attempt, 200);
            await Task.Delay(Random.Shared.Next(cap), ct); // экспонента + джиттер
        }
    }

    async Task<(long Qty, long Version)> GetAsync(long id, CancellationToken ct)
    {
        await using var cmd = db.CreateCommand("SELECT qty, version FROM stock WHERE id = @id");
        Add(cmd, "@id", id);
        await using var r = await cmd.ExecuteReaderAsync(ct);
        if (!await r.ReadAsync(ct)) throw new KeyNotFoundException();
        return (r.GetInt64(0), r.GetInt64(1));
    }

    async Task<bool> SaveAsync(long id, long qty, long version, CancellationToken ct)
    {
        await using var cmd = db.CreateCommand(
            "UPDATE stock SET qty = @qty, version = version + 1 WHERE id = @id AND version = @v");
        Add(cmd, "@qty", qty);
        Add(cmd, "@id", id);
        Add(cmd, "@v", version);
        return await cmd.ExecuteNonQueryAsync(ct) == 1; // RowsAffected
    }

    static void Add(DbCommand cmd, string name, object value)
    {
        var p = cmd.CreateParameter();
        p.ParameterName = name;
        p.Value = value;
        cmd.Parameters.Add(p);
    }
}

// Пессимистичная блокировка у EF Core API не имеет: FromSql("SELECT ... FOR UPDATE")
// внутри транзакции (PostgreSQL) или WITH (UPDLOCK, ROWLOCK) в SQL Server —
// тот же SQL, что в Go.
public static class Program
{
    public static void Main() => Console.WriteLine(typeof(StockRepo).Name);
}

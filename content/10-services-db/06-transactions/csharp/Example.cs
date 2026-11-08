using System.Data;
using System.Data.Common;
using System.Transactions;
using IsolationLevel = System.Data.IsolationLevel;

// Провайдера нет, чтобы код собирался без NuGet; источник создаёт
// NpgsqlDataSource.Create(connString) — аналог *sql.DB.
DbDataSource? db = null;
if (db is not null)
{
    var repo = new OrderRepo();

    // 1. TransactionScope — «окружающая» транзакция: каждое соединение, открытое
    // внутри блока, само в неё записывается. Код репозитория о транзакции не знает.
    // В Go такого механизма нет: транзакция не живёт в потоке или async-контексте.
    using (var scope = new TransactionScope(TransactionScopeAsyncFlowOption.Enabled))
    {
        await using var conn = await db.OpenConnectionAsync(); // попадёт в scope неявно
        await repo.ReserveAsync(conn, null, "A", 3);
        scope.Complete(); // без Complete — откат в Dispose, как defer tx.Rollback()
    }

    // 2. Явная транзакция ADO.NET — ровно то, что делают в Go: tx передаётся
    // параметром, и команда без cmd.Transaction в неё не попадёт.
    await using (var conn = await db.OpenConnectionAsync())
    await using (var tx = await conn.BeginTransactionAsync(IsolationLevel.RepeatableRead)) // sql.TxOptions{Isolation: ...}
    {
        await repo.ReserveAsync(conn, tx, "A", 3);
        await repo.ReserveAsync(conn, tx, "B", 1);
        await tx.CommitAsync(); // DisposeAsync без Commit откатит
    }

    // 3. EF Core: SaveChanges сам открывает транзакцию на все накопленные изменения
    // (change tracking). В Go отслеживания изменений нет — какие запросы
    // войдут в транзакцию, решает код явно.
}

public class OrderRepo
{
    // Принимает соединение и, возможно, транзакцию — аналог интерфейса DBTX в Go,
    // который реализуют и *sql.DB, и *sql.Tx.
    public async Task ReserveAsync(DbConnection conn, DbTransaction? tx, string sku, long qty)
    {
        await using var cmd = conn.CreateCommand();
        cmd.Transaction = tx;
        cmd.CommandText = "UPDATE stock SET qty = qty - @q WHERE sku = @s AND qty >= @q";
        Add(cmd, "@q", qty);
        Add(cmd, "@s", sku);
        if (await cmd.ExecuteNonQueryAsync() != 1)
            throw new InvalidOperationException("нет товара"); // исключение → Dispose → откат

        // NULL: DBNull.Value при записи, IsDBNull при чтении ↔ sql.Null[T] в Go.
        await using var log = conn.CreateCommand();
        log.Transaction = tx;
        log.CommandText = "INSERT INTO reservations (sku, qty, note) VALUES (@s, @q, @n)";
        Add(log, "@s", sku);
        Add(log, "@q", qty);
        Add(log, "@n", DBNull.Value);
        await log.ExecuteNonQueryAsync();
    }

    static void Add(DbCommand cmd, string name, object value)
    {
        var p = cmd.CreateParameter();
        p.ParameterName = name;
        p.Value = value;
        cmd.Parameters.Add(p);
    }
}

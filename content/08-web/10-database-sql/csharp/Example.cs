using System.Data;
using System.Data.Common;

// ADO.NET на абстракциях System.Data.Common. DbDataSource (.NET 7+) — аналог
// *sql.DB с пулом внутри; создаёт его провайдер: NpgsqlDataSource.Create(...)
// или DbProviderFactory.CreateDataSource(connString) для любого провайдера.
// Здесь провайдера нет, чтобы код собирался без NuGet.
DbDataSource? db = null;
if (db is not null)
{
    var repo = new AccountRepo(db); // один источник на приложение, как *sql.DB
    await repo.TransferAsync(1, 2, 30, CancellationToken.None);
    Console.WriteLine(await repo.GetAsync(1, CancellationToken.None));
}

public record Account(long Id, string Owner, string? Email, long Balance);

public class AccountRepo(DbDataSource db)
{
    public async Task<Account?> GetAsync(long id, CancellationToken ct)
    {
        // Команда от DbDataSource сама берёт соединение из пула на время выполнения —
        // как db.QueryContext в Go. Классический ADO.NET: OpenConnectionAsync + conn.CreateCommand.
        await using var cmd = db.CreateCommand("SELECT id, owner, email, balance FROM accounts WHERE id = @id");
        var p = cmd.CreateParameter(); // синтаксис плейсхолдера зависит от провайдера, как ? и $1 в Go
        p.ParameterName = "@id";
        p.Value = id;
        cmd.Parameters.Add(p);

        await using var reader = await cmd.ExecuteReaderAsync(ct); // defer rows.Close()
        if (!await reader.ReadAsync(ct))
            return null; // в Go — sql.ErrNoRows из Scan
        return new Account(
            reader.GetInt64(0),
            reader.GetString(1),
            reader.IsDBNull(2) ? null : reader.GetString(2), // sql.NullString / *string
            reader.GetInt64(3));
    }

    public async Task TransferAsync(long from, long to, long amount, CancellationToken ct)
    {
        // Для транзакции соединение берём явно: все команды должны идти через него.
        await using var conn = await db.OpenConnectionAsync(ct);
        // BeginTx + defer tx.Rollback(): DisposeAsync откатит незавершённую транзакцию.
        await using var tx = await conn.BeginTransactionAsync(IsolationLevel.Serializable, ct);
        await ExecAsync(conn, tx, "UPDATE accounts SET balance = balance - @a WHERE id = @id AND balance >= @a", amount, from, expectRows: 1, ct);
        await ExecAsync(conn, tx, "UPDATE accounts SET balance = balance + @a WHERE id = @id", amount, to, expectRows: 1, ct);
        await tx.CommitAsync(ct);
    }

    static async Task ExecAsync(DbConnection conn, DbTransaction tx, string sql, long amount, long id, int expectRows, CancellationToken ct)
    {
        await using var cmd = conn.CreateCommand();
        cmd.Transaction = tx; // в Go — запросы через tx, а не через db
        cmd.CommandText = sql;
        foreach (var (name, value) in new[] { ("@a", amount), ("@id", id) })
        {
            var p = cmd.CreateParameter();
            p.ParameterName = name;
            p.Value = value;
            cmd.Parameters.Add(p);
        }
        // ExecuteNonQuery возвращает RowsAffected
        if (await cmd.ExecuteNonQueryAsync(ct) != expectRows)
            throw new InvalidOperationException("недостаточно средств или нет счёта");
    }
}

// С Dapper то же короче, и это ближе всего к Go-стилю:
//   conn.QuerySingleOrDefaultAsync<Account>("SELECT ... WHERE id = @id", new { id });
// EF Core (DbContext, LINQ, change tracking) в Go аналога в stdlib не имеет.

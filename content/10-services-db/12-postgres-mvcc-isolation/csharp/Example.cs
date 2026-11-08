using System.Data;
using Npgsql;

// Уровни изоляции те же — их реализует сервер, а не драйвер. По умолчанию
// и в Npgsql, и в pgx — Read Committed (настройка сервера default_transaction_isolation).
var connString = Environment.GetEnvironmentVariable("POSTGRES_DSN_NPGSQL");
if (connString is null)
{
    Console.WriteLine("нужна строка подключения Npgsql");
    return;
}
await using var db = NpgsqlDataSource.Create(connString);

// Write skew под Serializable: одна из двух транзакций получит 40001,
// повтор увидит одного дежурного и откажет.
var ok = await Tx.RunAsync(db, IsolationLevel.Serializable, maxAttempts: 5, async (conn, tx, ct) =>
{
    await using var count = new NpgsqlCommand("SELECT count(*) FROM doctors WHERE on_call", conn, tx);
    if ((long)(await count.ExecuteScalarAsync(ct))! < 2)
        return false;
    await using var off = new NpgsqlCommand("UPDATE doctors SET on_call = false WHERE name = $1", conn, tx);
    off.Parameters.Add(new() { Value = "alice" });
    await off.ExecuteNonQueryAsync(ct);
    return true;
}, CancellationToken.None);
Console.WriteLine($"alice снята с дежурства: {ok}");

public static class Tx
{
    // Аналог RunInTx: транзакция передаётся явно (conn + tx в каждую команду),
    // как pgx.Tx в Go. TransactionScope с «окружающей» транзакцией в Go не бывает.
    public static async Task<T> RunAsync<T>(
        NpgsqlDataSource db, IsolationLevel iso, int maxAttempts,
        Func<NpgsqlConnection, NpgsqlTransaction, CancellationToken, Task<T>> fn,
        CancellationToken ct)
    {
        for (var attempt = 1; ; attempt++)
        {
            await using var conn = await db.OpenConnectionAsync(ct);
            // DisposeAsync откатывает незафиксированную транзакцию — это defer tx.Rollback(ctx).
            await using var tx = await conn.BeginTransactionAsync(iso, ct);
            try
            {
                var result = await fn(conn, tx, ct);
                await tx.CommitAsync(ct); // под Serializable 40001 бывает и здесь
                return result;
            }
            catch (PostgresException ex) when (
                ex.SqlState is PostgresErrorCodes.SerializationFailure or PostgresErrorCodes.DeadlockDetected
                && attempt < maxAttempts)
            {
                // Повторяем всю транзакцию с новым снимком. В EF Core то же делает
                // EnableRetryOnFailure (NpgsqlRetryingExecutionStrategy).
            }
        }
    }
}

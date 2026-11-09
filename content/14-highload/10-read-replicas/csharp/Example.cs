using Npgsql;
using NpgsqlTypes;

// Npgsql умеет несколько хостов в одной строке подключения и сам выбирает
// основной или реплику по Target Session Attributes. Задержку репликации он
// не учитывает: read-your-writes — по-прежнему ваша забота, как и в Go.
var hosts = Environment.GetEnvironmentVariable("POSTGRES_HOSTS_NPGSQL"); // "Host=pg1,pg2;Username=...;Database=..."
if (hosts is null)
{
    Console.WriteLine("нужна строка подключения Npgsql с несколькими хостами");
    return;
}

await using var multi = new NpgsqlDataSourceBuilder(hosts).BuildMultiHost();
await using var primary = multi.WithTargetSession(TargetSessionAttributes.Primary);
await using var replicas = multi.WithTargetSession(TargetSessionAttributes.PreferStandby);

// Токен после COMMIT — позиция WAL основного.
await using (var insert = primary.CreateCommand("INSERT INTO orders (customer_id, total) VALUES (7, 300)"))
    await insert.ExecuteNonQueryAsync();
await using var lsnCmd = primary.CreateCommand("SELECT pg_current_wal_lsn()");
var token = (NpgsqlLogSequenceNumber)(await lsnCmd.ExecuteScalarAsync())!;

// Реплика догнала токен — читаем с неё, иначе с основного.
await using var conn = await replicas.OpenConnectionAsync();
await using var replayCmd = new NpgsqlCommand("SELECT pg_last_wal_replay_lsn()", conn);
var replayed = await replayCmd.ExecuteScalarAsync() is NpgsqlLogSequenceNumber r && r >= token;
var source = replayed ? replicas : primary;

await using var history = source.CreateCommand("SELECT count(*) FROM orders WHERE customer_id = $1");
history.Parameters.Add(new() { Value = 7L });
Console.WriteLine($"заказов: {await history.ExecuteScalarAsync()}, с реплики: {replayed}");

// Сравнение LSN — числовое: NpgsqlLogSequenceNumber, как LSN uint64 в Go.
Console.WriteLine(NpgsqlLogSequenceNumber.Parse("0/A") < NpgsqlLogSequenceNumber.Parse("0/10")); // True

using System.Data.Common;
using System.Text.Json;

// С MassTransit outbox включается конфигурацией (нужен NuGet, поэтому в комментарии):
//   x.AddEntityFrameworkOutbox<ShopDbContext>(o => o.UsePostgres().UseBusOutbox());
//   db.Orders.Add(order); await publishEndpoint.Publish(new OrderCreated(...)); await db.SaveChangesAsync();
// SaveChanges пишет сообщение в таблицу вместе с заказом, фоновый сервис отправляет.
// Ниже — то же руками на ADO.NET, как в Go-примере.

DbDataSource? source = null; // NpgsqlDataSource.Create(...) в настоящем приложении
if (source is not null)
{
    var id = await Orders.CreateAsync(source, "Анна", 300, CancellationToken.None);
    var relay = new Relay(source, new ConsolePublisher());
    Console.WriteLine($"{id}: опубликовано {await relay.RunOnceAsync(100, CancellationToken.None)}");
}

public record OrderCreated(long OrderId, string Customer, long Total);
public record OutboxEvent(long Id, string Topic, string Key, byte[] Payload);

public interface IPublisher
{
    Task PublishAsync(OutboxEvent e, CancellationToken ct);
}

public class ConsolePublisher : IPublisher
{
    public Task PublishAsync(OutboxEvent e, CancellationToken ct)
    {
        Console.WriteLine($"{e.Topic} {e.Key}");
        return Task.CompletedTask;
    }
}

public static class Orders
{
    public static async Task<long> CreateAsync(DbDataSource db, string customer, long total, CancellationToken ct)
    {
        await using var conn = await db.OpenConnectionAsync(ct);
        await using var tx = await conn.BeginTransactionAsync(ct); // как BeginTx + defer Rollback
        var id = (long)(await Sql.Cmd(conn, tx, "INSERT INTO orders (customer, total) VALUES (@c, @t) RETURNING id",
            ("@c", customer), ("@t", total)).ExecuteScalarAsync(ct))!;
        var payload = JsonSerializer.SerializeToUtf8Bytes(new OrderCreated(id, customer, total));
        // Та же транзакция: cmd.Transaction = tx. Без него команда ушла бы мимо транзакции.
        await Sql.Cmd(conn, tx, "INSERT INTO outbox (topic, key, payload) VALUES ('order.created', @k, @p)",
            ("@k", id.ToString()), ("@p", payload)).ExecuteNonQueryAsync(ct);
        await tx.CommitAsync(ct);
        return id;
    }
}

// В ASP.NET Core relay — BackgroundService с PeriodicTimer; в Go — горутина с time.Ticker.
public class Relay(DbDataSource db, IPublisher pub)
{
    public async Task<int> RunOnceAsync(int limit, CancellationToken ct)
    {
        var batch = new List<OutboxEvent>();
        await using (var cmd = db.CreateCommand("SELECT id, topic, key, payload FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT @n"))
        {
            var p = cmd.CreateParameter();
            p.ParameterName = "@n";
            p.Value = limit;
            cmd.Parameters.Add(p);
            await using var r = await cmd.ExecuteReaderAsync(ct);
            while (await r.ReadAsync(ct))
                batch.Add(new OutboxEvent(r.GetInt64(0), r.GetString(1), r.GetString(2), (byte[])r.GetValue(3)));
        } // reader закрыт до публикации — соединение вернулось в пул

        var done = 0;
        foreach (var e in batch)
        {
            await pub.PublishAsync(e, ct); // исключение остановит проход: порядок сохраняется
            await using var conn = await db.OpenConnectionAsync(ct);
            await Sql.Cmd(conn, null, "UPDATE outbox SET published_at = CURRENT_TIMESTAMP WHERE id = @id", ("@id", e.Id))
                .ExecuteNonQueryAsync(ct);
            done++;
        }
        return done;
    }
}

static class Sql
{
    public static DbCommand Cmd(DbConnection conn, DbTransaction? tx, string text, params (string Name, object Value)[] args)
    {
        var cmd = conn.CreateCommand();
        cmd.Transaction = tx;
        cmd.CommandText = text;
        foreach (var (name, value) in args)
        {
            var p = cmd.CreateParameter();
            p.ParameterName = name;
            p.Value = value;
            cmd.Parameters.Add(p);
        }
        return cmd;
    }
}

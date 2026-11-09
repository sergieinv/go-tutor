using Npgsql;

// Идемпотентный потребитель на Npgsql: отметка «обработано» и изменение
// модели чтения — в одной транзакции. MassTransit и NServiceBus делают то же
// самое своим inbox'ом; в Go это пишут руками, как в примере урока.
var connString = Environment.GetEnvironmentVariable("POSTGRES_DSN_NPGSQL");
if (connString is null)
{
    Console.WriteLine("нужна строка подключения Npgsql");
    return;
}
await using var db = NpgsqlDataSource.Create(connString);

var evt = new OrderCreated("e1", "anna", 100);
Console.WriteLine(await Handle(db, evt)); // True
Console.WriteLine(await Handle(db, evt)); // False — повтор той же доставки

static async Task<bool> Handle(NpgsqlDataSource db, OrderCreated e, CancellationToken ct = default)
{
    await using var conn = await db.OpenConnectionAsync(ct);
    await using var tx = await conn.BeginTransactionAsync(ct);

    await using (var mark = new NpgsqlCommand(
        "INSERT INTO processed (event_id) VALUES ($1) ON CONFLICT DO NOTHING", conn, tx))
    {
        mark.Parameters.Add(new() { Value = e.Id });
        if (await mark.ExecuteNonQueryAsync(ct) == 0)
            return false; // уже обработано: подтверждаем сообщение, ничего не меняя
    }

    await using (var project = new NpgsqlCommand(
        """
        INSERT INTO balances (customer, total, orders) VALUES ($1, $2, 1)
        ON CONFLICT (customer) DO UPDATE
        SET total = balances.total + excluded.total, orders = balances.orders + 1
        """, conn, tx))
    {
        project.Parameters.Add(new() { Value = e.Customer });
        project.Parameters.Add(new() { Value = e.Amount });
        await project.ExecuteNonQueryAsync(ct);
    }

    await tx.CommitAsync(ct); // не дошли сюда — откат, и повтор применит всё заново
    return true;
}

record OrderCreated(string Id, string Customer, long Amount);

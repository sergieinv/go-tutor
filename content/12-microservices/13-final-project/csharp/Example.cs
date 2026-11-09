// Та же система на .NET собирается из готовых блоков (NuGet), а не из своего кода:
//
//   // Aspire AppHost — вместо docker-compose.yml: сервисы, базы, Kafka, дашборд с трассами.
//   var pg = builder.AddPostgres("pg").AddDatabase("orders");
//   var kafka = builder.AddKafka("kafka");
//   var warehouse = builder.AddProject<Projects.Warehouse>("warehouse").WithReference(builder.AddMySql("my"));
//   builder.AddProject<Projects.Orders>("orders").WithReference(pg).WithReference(kafka).WithReference(warehouse);
//
//   // ServiceDefaults: OpenTelemetry (трассы, метрики, OTLP), health checks, service discovery.
//   builder.AddServiceDefaults();
//   builder.Services.AddGrpcClient<Warehouse.WarehouseClient>(o => o.Address = new("https://warehouse"))
//       .AddStandardResilienceHandler();   // Polly: таймаут, повторы, circuit breaker
//
//   // MassTransit: transactional outbox (EF Core) и консьюмеры с повторами; Kafka — через Rider.
//   services.AddMassTransit(x => {
//       x.AddEntityFrameworkOutbox<OrdersDb>(o => { o.UsePostgres(); o.UseBusOutbox(); });
//       x.AddRider(r => {
//           r.AddConsumer<OrderCreatedConsumer>();
//           r.UsingKafka((ctx, k) => k.TopicEndpoint<OrderCreated>("orders.events", "notifications",
//               e => e.ConfigureConsumer<OrderCreatedConsumer>(ctx)));
//       });
//   });
//
// Ниже — то, что ни один из них не решит за вас: какая ошибка соседа требует компенсации.
// Это задание урока на C#. Grpc.Core здесь заменён заглушкой, чтобы файл собирался без NuGet.

var saga = new OrderSaga(new FlakyWarehouse(), new MemoryQueue());
try
{
    await saga.CreateOrderAsync(orderId: 4, CancellationToken.None);
}
catch (RpcException e)
{
    Console.WriteLine($"ответ клиенту: {e.StatusCode}"); // Unavailable, Release поставлен в очередь
}

// --- заглушки Grpc.Core ---
public enum StatusCode { FailedPrecondition, InvalidArgument, Unavailable, DeadlineExceeded, Internal }

public class RpcException(StatusCode code) : Exception(code.ToString())
{
    public StatusCode StatusCode { get; } = code;
}

public interface IWarehouse
{
    Task ReserveAsync(long orderId, CancellationToken ct);
    Task ReleaseAsync(long orderId, CancellationToken ct);
}

public interface IReleaseQueue
{
    Task AddAsync(long orderId, CancellationToken ct);
}

public class OrderSaga(IWarehouse warehouse, IReleaseQueue queue)
{
    public static readonly TimeSpan ReleaseTimeout = TimeSpan.FromSeconds(5);

    public async Task CreateOrderAsync(long orderId, CancellationToken ct)
    {
        try
        {
            await warehouse.ReserveAsync(orderId, ct);
        }
        // Точный отказ склада: резерва нет — компенсировать нечего.
        catch (RpcException e) when (e.StatusCode is StatusCode.FailedPrecondition or StatusCode.InvalidArgument)
        {
            throw;
        }
        // Таймаут, обрыв, отмена: исход неизвестен — компенсация.
        catch (Exception)
        {
            await CompensateAsync(orderId);
            throw;
        }
        // ... шаг 2: заказ и событие в outbox (MassTransit: SaveChangesAsync с UseBusOutbox).
    }

    // В Go значения запроса и отмена едут в одном ctx, поэтому нужен context.WithoutCancel.
    // В .NET они разделены: Activity.Current (трасса) течёт через AsyncLocal сама,
    // а отмену мы просто не передаём — новый токен со своим таймаутом.
    private async Task CompensateAsync(long orderId)
    {
        using var cts = new CancellationTokenSource(ReleaseTimeout);
        try
        {
            await warehouse.ReleaseAsync(orderId, cts.Token);
        }
        catch (Exception)
        {
            using var qcts = new CancellationTokenSource(ReleaseTimeout);
            await queue.AddAsync(orderId, qcts.Token); // фоновый воркер повторит Release
        }
    }
}

public class FlakyWarehouse : IWarehouse
{
    public Task ReserveAsync(long orderId, CancellationToken ct) => throw new RpcException(StatusCode.Unavailable);
    public Task ReleaseAsync(long orderId, CancellationToken ct) => throw new RpcException(StatusCode.Unavailable);
}

public class MemoryQueue : IReleaseQueue
{
    public Task AddAsync(long orderId, CancellationToken ct)
    {
        Console.WriteLine($"Release({orderId}) — в очередь компенсаций");
        return Task.CompletedTask;
    }
}

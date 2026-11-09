// ASP.NET Core: те же три роли, но соседей собирает DI-контейнер.
// В настоящем проекте клиент «Склада» генерирует Grpc.Tools из warehouse.proto
// (<Protobuf Include="warehouse.proto" GrpcServices="Client" /> в .csproj),
// а регистрирует Grpc.Net.ClientFactory:
//   builder.Services.AddGrpcClient<WarehouseService.WarehouseServiceClient>(
//       o => o.Address = new Uri("http://warehouse:8080"));
// Продюсер — Confluent.Kafka (IProducer<string, string>) или MassTransit.
// Здесь вместо них интерфейсы, чтобы файл собирался без NuGet.
// В Go всё это — несколько строк в main: grpc.NewClient, kgo.NewClient,
// &orders{stock: ..., events: ...}.

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddSingleton<IWarehouseClient, FakeWarehouse>();
builder.Services.AddSingleton<IEventPublisher, ConsolePublisher>();
builder.Services.AddSingleton<OrdersService>();
var app = builder.Build();

app.MapPost("/orders", async (OrderRequest req, OrdersService svc, CancellationToken ct) =>
{
    var result = await svc.PlaceAsync(req, ct);
    return result is null
        ? Results.Conflict("out of stock")
        : Results.Created($"/orders/{result}", new { id = result });
});

app.Run();

public record OrderLine(string Sku, int Qty);
public record OrderRequest(string Customer, List<OrderLine> Lines);
public record OrderCreated(string OrderId, string Customer);

// Контракт соседа — то, что «Заказы» знают о «Складе».
public interface IWarehouseClient
{
    Task<bool> ReserveAsync(string orderId, IReadOnlyList<OrderLine> lines, CancellationToken ct);
}

public interface IEventPublisher
{
    Task PublishAsync(OrderCreated e, CancellationToken ct);
}

public class OrdersService(IWarehouseClient warehouse, IEventPublisher events)
{
    private int _seq;

    public async Task<string?> PlaceAsync(OrderRequest req, CancellationToken ct)
    {
        var id = $"o-{Interlocked.Increment(ref _seq)}";
        // Синхронно: ждём «Склад» (в Go — дедлайн в context.WithTimeout).
        if (!await warehouse.ReserveAsync(id, req.Lines, ct))
            return null;
        // Асинхронно: событие в Kafka; читатели «Заказам» неизвестны.
        await events.PublishAsync(new OrderCreated(id, req.Customer), ct);
        return id;
    }
}

public class FakeWarehouse : IWarehouseClient
{
    public Task<bool> ReserveAsync(string orderId, IReadOnlyList<OrderLine> lines, CancellationToken ct) =>
        Task.FromResult(lines.All(l => l.Qty <= 2));
}

public class ConsolePublisher : IEventPublisher
{
    public Task PublishAsync(OrderCreated e, CancellationToken ct)
    {
        Console.WriteLine($"order.created {e.OrderId} {e.Customer}");
        return Task.CompletedTask;
    }
}

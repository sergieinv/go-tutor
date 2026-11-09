using Grpc.Core;
using Grpc.Net.Client;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Orders.V1; // классы, сгенерированные Grpc.Tools из того же orders.proto

// Проект: Microsoft.NET.Sdk.Web + пакеты Grpc.AspNetCore (сервер) и
// Grpc.Net.Client; генерацию включает строка в .csproj:
//   <Protobuf Include="orders.proto" GrpcServices="Both" />
// Go: buf generate кладёт код рядом с .proto, сборка про .proto не знает.
var builder = WebApplication.CreateBuilder(args);
// h2c: без TLS Kestrel по умолчанию отвечает только по HTTP/1.1, а gRPC нужен HTTP/2.
builder.WebHost.ConfigureKestrel(k => k.ListenLocalhost(5000, o => o.Protocols = HttpProtocols.Http2));
builder.Services.AddGrpc(o =>
{
    o.MaxReceiveMessageSize = 1 << 20; // Go: grpc.MaxRecvMsgSize(1 << 20)
});
// Сервис gRPC в ASP.NET Core по умолчанию создаётся на каждый вызов,
// поэтому состояние живёт в singleton. В Go обработчики — методы одной
// структуры, которую вы сами создали и зарегистрировали.
builder.Services.AddSingleton<OrderStore>();
// GracefulStop: хост ждёт активные вызовы не дольше ShutdownTimeout.
builder.Services.Configure<HostOptions>(o => o.ShutdownTimeout = TimeSpan.FromSeconds(10));

var app = builder.Build();
app.MapGrpcService<OrderServiceImpl>(); // Go: RegisterOrderServiceServer(srv, impl)
await app.StartAsync();

// Клиент: канал тоже ленивый, h2c без TLS — только для локальной отладки.
using var channel = GrpcChannel.ForAddress("http://localhost:5000");
var client = new OrderService.OrderServiceClient(channel);
var deadline = DateTime.UtcNow.AddSeconds(5); // Go: context.WithTimeout
var created = await client.CreateOrderAsync(new CreateOrderRequest
{
    Customer = "alice",
    Items = { new Item { Sku = "tea", Quantity = 2, PriceCents = 350 } },
}, deadline: deadline);
Console.WriteLine($"создан: {created.Order.Id} {created.Order.TotalCents}");

try
{
    await client.CancelOrderAsync(new CancelOrderRequest { Id = "ord-1" }, deadline: deadline);
}
catch (RpcException ex) // Go: ошибка-значение, status.Code(err)
{
    Console.WriteLine($"CancelOrder: {ex.StatusCode}"); // Unimplemented
}

await app.StopAsync();

public class OrderStore
{
    private readonly object _lock = new();
    private readonly Dictionary<string, Order> _orders = new();

    public Order Add(Order o)
    {
        lock (_lock)
        {
            o.Id = $"ord-{_orders.Count + 1}";
            _orders[o.Id] = o;
            return o;
        }
    }

    public Order? Find(string id)
    {
        lock (_lock) return _orders.GetValueOrDefault(id);
    }
}

// Базовый класс OrderServiceBase — аналог UnimplementedOrderServiceServer:
// всё, что не переопределено (здесь CancelOrder), бросает
// RpcException(StatusCode.Unimplemented).
public class OrderServiceImpl(OrderStore store) : OrderService.OrderServiceBase
{
    public override Task<CreateOrderResponse> CreateOrder(CreateOrderRequest request, ServerCallContext context)
    {
        if (request.Customer == "" || request.Items.Count == 0)
            throw new RpcException(new Status(StatusCode.InvalidArgument, "customer and items are required"));
        var order = new Order { Customer = request.Customer };
        order.Items.AddRange(request.Items);
        order.TotalCents = request.Items.Sum(i => i.Quantity * i.PriceCents);
        return Task.FromResult(new CreateOrderResponse { Order = store.Add(order) });
    }

    public override Task<GetOrderResponse> GetOrder(GetOrderRequest request, ServerCallContext context)
    {
        var order = store.Find(request.Id)
            ?? throw new RpcException(new Status(StatusCode.NotFound, $"order \"{request.Id}\" not found"));
        return Task.FromResult(new GetOrderResponse { Order = order });
    }
}

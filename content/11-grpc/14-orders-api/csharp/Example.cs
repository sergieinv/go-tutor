// Пакеты: Grpc.AspNetCore, Microsoft.AspNetCore.Grpc.JsonTranscoding, Grpc.StatusProto.
// Контракт подключается в .csproj, код генерирует Grpc.Tools при сборке:
//   <Protobuf Include="orders.proto" GrpcServices="Server" />
// Правил buf.validate C#-генератор не понимает: их проверяют пакетом
// protovalidate-net или дублируют в FluentValidation.
using System.Collections.Concurrent;
using Google.Protobuf.WellKnownTypes;
using Google.Rpc;
using Grpc.Core;
using Grpc.Core.Interceptors;
using Orders.V1;
using Status = Google.Rpc.Status;

var builder = WebApplication.CreateBuilder(args);
builder.Services
    .AddGrpc(o =>
    {
        o.MaxReceiveMessageSize = 1 << 20;
        o.Interceptors.Add<ValidationInterceptor>();
    })
    // REST из google.api.http — аналог grpc-gateway, но в том же процессе
    // и в том же конвейере: interceptors работают и для REST-вызовов.
    .AddJsonTranscoding();
builder.Services.AddSingleton<OrderStore>();
builder.Services.AddSingleton(TimeProvider.System); // часы внедряются, как now в Go

var app = builder.Build();
app.MapGrpcService<OrdersService>();
if (args.Contains("--serve"))
    app.Run();

public class OrderStore
{
    public readonly object Lock = new();
    public readonly List<Order> Orders = [];
    public readonly ConcurrentDictionary<string, (CreateOrderRequest Req, string Id)> Keys = new();
}

public class OrdersService(OrderStore store, TimeProvider clock) : OrderService.OrderServiceBase
{
    public override Task<CreateOrderResponse> CreateOrder(CreateOrderRequest request, ServerCallContext context)
    {
        // Ключ идемпотентности — заголовок запроса. В JSON transcoding HTTP-заголовки
        // доходят до RequestHeaders сами; в Go их пропускает HeaderMatcher gateway.
        var key = context.RequestHeaders.GetValue("idempotency-key");
        lock (store.Lock)
        {
            if (key is not null && store.Keys.TryGetValue(key, out var seen))
            {
                if (!seen.Req.Equals(request)) // сгенерированный Equals — как proto.Equal
                    throw new RpcException(new Grpc.Core.Status(StatusCode.AlreadyExists, "idempotency key reused"));
                return Task.FromResult(new CreateOrderResponse { Order = store.Orders.First(o => o.Id == seen.Id).Clone() });
            }
            var order = new Order
            {
                Id = $"ord-{store.Orders.Count + 1}",
                CustomerId = request.CustomerId,
                Items = { request.Items.Select(i => i.Clone()) },
                TotalCents = request.Items.Sum(i => i.Quantity * i.PriceCents),
                Status = OrderStatus.Created,
                CreateTime = Timestamp.FromDateTimeOffset(clock.GetUtcNow()),
            };
            store.Orders.Add(order);
            if (key is not null)
                store.Keys[key] = (request.Clone(), order.Id);
            return Task.FromResult(new CreateOrderResponse { Order = order.Clone() });
        }
    }

    public override Task<CancelOrderResponse> CancelOrder(CancelOrderRequest request, ServerCallContext context)
    {
        lock (store.Lock)
        {
            var order = store.Orders.FirstOrDefault(o => o.Id == request.Id)
                ?? throw new RpcException(new Grpc.Core.Status(StatusCode.NotFound, $"order {request.Id} not found"));
            if (order.Status == OrderStatus.Cancelled)
                throw new RpcException(new Grpc.Core.Status(StatusCode.FailedPrecondition, "order already cancelled"));
            order.Status = OrderStatus.Cancelled;
            return Task.FromResult(new CancelOrderResponse { Order = order.Clone() });
        }
    }
    // GetOrder и ListOrders — так же; без override метод отвечает Unimplemented,
    // как встроенный UnimplementedOrderServiceServer в Go.
}

// Валидация в interceptor с ответом InvalidArgument + BadRequest — тот же
// приём, что в Go; детали упаковывает Grpc.StatusProto (ToRpcException).
public class ValidationInterceptor : Interceptor
{
    public override Task<TResponse> UnaryServerHandler<TRequest, TResponse>(
        TRequest request, ServerCallContext context, UnaryServerMethod<TRequest, TResponse> continuation)
    {
        if (request is CreateOrderRequest { CustomerId: "" })
        {
            var badRequest = new BadRequest
            {
                FieldViolations = { new BadRequest.Types.FieldViolation { Field = "customer_id", Description = "value is required" } },
            };
            throw new Status
            {
                Code = (int)Code.InvalidArgument,
                Message = "invalid request",
                Details = { Any.Pack(badRequest) },
            }.ToRpcException();
        }
        return continuation(request, context);
    }
}

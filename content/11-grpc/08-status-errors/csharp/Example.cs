using Google.Protobuf.WellKnownTypes;
using Google.Rpc;
using Grpc.Core;
using Grpc.Core.Interceptors;
using Grpc.Net.Client;
using Orders.V1; // сгенерировано Grpc.Tools из того же orders.proto
using Status = Google.Rpc.Status;
using Microsoft.AspNetCore.Server.Kestrel.Core;

// Rich error model в .NET — пакет Grpc.StatusProto: Google.Rpc.Status
// с деталями превращается в RpcException (ToRpcException) и обратно
// (GetRpcStatus). Go: status.New(...).WithDetails(...) и st.Details().
var builder = WebApplication.CreateBuilder(args);
// h2c: без TLS Kestrel по умолчанию отвечает только по HTTP/1.1, а gRPC нужен HTTP/2.
builder.WebHost.ConfigureKestrel(k => k.ListenLocalhost(5000, o => o.Protocols = HttpProtocols.Http2));
// Перевод доменных исключений — в одном месте, interceptor'е.
builder.Services.AddGrpc(o => o.Interceptors.Add<DomainErrorInterceptor>());
var app = builder.Build();
app.MapGrpcService<OrdersImpl>();
await app.StartAsync();

using var channel = GrpcChannel.ForAddress("http://localhost:5000");
var client = new OrderService.OrderServiceClient(channel);
try
{
    await client.CreateOrderAsync(new CreateOrderRequest(), deadline: DateTime.UtcNow.AddSeconds(5));
}
catch (RpcException ex)
{
    var st = ex.GetRpcStatus(); // Go: status.Convert(err)
    Console.WriteLine($"{ex.StatusCode}: {st?.Message}");
    foreach (var v in st?.GetDetail<BadRequest>()?.FieldViolations ?? [])
        Console.WriteLine($"  поле {v.Field}: {v.Description}");
    Console.WriteLine($"  повторять: {ShouldRetry(ex)}");
}
await app.StopAsync();

static bool ShouldRetry(RpcException ex) => ex.StatusCode switch
{
    StatusCode.Unavailable or StatusCode.Aborted => true,
    StatusCode.ResourceExhausted => ex.GetRpcStatus()?.GetDetail<RetryInfo>() is not null,
    _ => false,
};

public class ValidationException(params (string Field, string Reason)[] fields) : Exception("validation failed")
{
    public (string Field, string Reason)[] Fields { get; } = fields;
}

public class NotFoundException() : Exception("order not found");

public class OrdersImpl : OrderService.OrderServiceBase
{
    public override Task<CreateOrderResponse> CreateOrder(CreateOrderRequest request, ServerCallContext context)
    {
        if (request.Customer == "")
            throw new ValidationException(("customer", "must not be empty"));
        return Task.FromResult(new CreateOrderResponse { Order = new Order { Id = "ord-1" } });
    }

    public override Task<GetOrderResponse> GetOrder(GetOrderRequest request, ServerCallContext context) =>
        throw new NotFoundException();
}

// В ASP.NET Core необработанное исключение в gRPC-сервисе становится
// StatusCode.Unknown с текстом "Exception was thrown by handler." (детали —
// только при EnableDetailedErrors). В Go обычная ошибка уходит как Unknown
// вместе с err.Error() — поэтому перевод в статус в Go обязателен.
public class DomainErrorInterceptor : Interceptor
{
    public override async Task<TResponse> UnaryServerHandler<TRequest, TResponse>(TRequest request,
        ServerCallContext context, UnaryServerMethod<TRequest, TResponse> continuation)
    {
        try
        {
            return await continuation(request, context);
        }
        catch (ValidationException ex)
        {
            var br = new BadRequest();
            foreach (var (field, reason) in ex.Fields)
                br.FieldViolations.Add(new BadRequest.Types.FieldViolation { Field = field, Description = reason });
            throw new Status
            {
                Code = (int)Code.InvalidArgument, Message = "invalid request", Details = { Any.Pack(br) },
            }.ToRpcException();
        }
        catch (NotFoundException)
        {
            var info = new ErrorInfo { Reason = "ORDER_NOT_FOUND", Domain = "orders.example.com" };
            throw new Status { Code = (int)Code.NotFound, Message = "order not found", Details = { Any.Pack(info) } }
                .ToRpcException();
        }
    }
}

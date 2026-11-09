using Grpc.Core;
using Grpc.Net.Client;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Orders.V1; // сгенерировано Grpc.Tools из того же checkout.proto

var builder = WebApplication.CreateBuilder(args);
// h2c: без TLS Kestrel по умолчанию отвечает только по HTTP/1.1, а gRPC нужен HTTP/2.
builder.WebHost.ConfigureKestrel(k => k.ListenLocalhost(5000, o => o.Protocols = HttpProtocols.Http2));
builder.Services.AddGrpc();
// Клиент Stock из DI (Grpc.Net.ClientFactory). EnableCallContextPropagation()
// сам переносит дедлайн и отмену входящего вызова в исходящий — в Go это
// делает передача того же ctx.
builder.Services.AddGrpcClient<StockService.StockServiceClient>(o => o.Address = new Uri("http://localhost:5000"))
    .EnableCallContextPropagation();
var app = builder.Build();
app.MapGrpcService<CheckoutImpl>();
app.MapGrpcService<StockImpl>();
await app.StartAsync();

using var channel = GrpcChannel.ForAddress("http://localhost:5000");
var client = new CheckoutService.CheckoutServiceClient(channel);

// Metadata ↔ Grpc.Core.Metadata; ключ с суффиксом -bin — байтовое значение.
var headers = new Metadata
{
    { "x-request-id", "req-42" },
    { "trace-bin", new byte[] { 0xca, 0xfe } },
};
// Дедлайн — абсолютное время у вызова (Go: context.WithTimeout).
using var call = client.CheckoutAsync(new CheckoutRequest { OrderId = "ord-1", Sku = "tea", Quantity = 1 },
    headers, deadline: DateTime.UtcNow.AddSeconds(2));
var res = await call.ResponseAsync;
var header = await call.ResponseHeadersAsync; // Go: grpc.Header(&md)
Console.WriteLine($"клиент: {res.ReservationId} {header.GetValue("x-request-id")} {call.GetTrailers().GetValue("x-stock-left")}");

try
{
    await client.CheckoutAsync(new CheckoutRequest { OrderId = "ord-2", Sku = "tea", Quantity = 1 },
        headers, deadline: DateTime.UtcNow.AddMilliseconds(50));
}
catch (RpcException ex) when (ex.StatusCode == StatusCode.DeadlineExceeded)
{
    Console.WriteLine("клиент: DeadlineExceeded");
}

await app.StopAsync();

public class CheckoutImpl(StockService.StockServiceClient stock) : CheckoutService.CheckoutServiceBase
{
    public override async Task<CheckoutResponse> Checkout(CheckoutRequest request, ServerCallContext context)
    {
        // Входящая metadata ↔ metadata.FromIncomingContext(ctx).
        var reqId = context.RequestHeaders.GetValue("x-request-id") ?? "";
        // context.Deadline ↔ ctx.Deadline(); без дедлайна — DateTime.MaxValue.
        if (context.Deadline - DateTime.UtcNow < TimeSpan.FromMilliseconds(100))
            throw new RpcException(new Status(StatusCode.DeadlineExceeded, "not enough time"));

        await context.WriteResponseHeadersAsync(new Metadata { { "x-request-id", reqId } }); // grpc.SetHeader
        // Metadata, как и в Go, не пробрасывается сама — только то, что передали.
        var res = await stock.ReserveAsync(
            new ReserveRequest { OrderId = request.OrderId, Sku = request.Sku, Quantity = request.Quantity },
            new Metadata { { "x-request-id", reqId } });
        context.ResponseTrailers.Add("x-stock-left", "7"); // grpc.SetTrailer
        return new CheckoutResponse { OrderId = request.OrderId, ReservationId = res.ReservationId };
    }
}

public class StockImpl : StockService.StockServiceBase
{
    public override async Task<ReserveResponse> Reserve(ReserveRequest request, ServerCallContext context)
    {
        // CancellationToken срабатывает и по дедлайну, и по отмене клиента —
        // как ctx.Done() в Go.
        await Task.Delay(TimeSpan.FromMilliseconds(10), context.CancellationToken);
        return new ReserveResponse { ReservationId = "res-" + request.OrderId };
    }
}

using Grpc.Core;
using Grpc.Net.Client;
using Microsoft.AspNetCore.Server.Kestrel.Core;
using Orders.V1; // сгенерировано Grpc.Tools из того же orders.proto

var builder = WebApplication.CreateBuilder(args);
// h2c: без TLS Kestrel по умолчанию отвечает только по HTTP/1.1, а gRPC нужен HTTP/2.
builder.WebHost.ConfigureKestrel(k => k.ListenLocalhost(5000, o => o.Protocols = HttpProtocols.Http2));
builder.Services.AddGrpc();
var app = builder.Build();
app.MapGrpcService<OrderStreamImpl>();
await app.StartAsync();

using var channel = GrpcChannel.ForAddress("http://localhost:5000");
var client = new OrderStreamService.OrderStreamServiceClient(channel);

// Server streaming: ResponseStream — IAsyncStreamReader, читается await foreach.
// Go: цикл Recv() до io.EOF.
using (var watch = client.WatchOrders(new WatchOrdersRequest { Customer = "alice" }))
{
    await foreach (var ev in watch.ResponseStream.ReadAllAsync())
        Console.WriteLine($"событие: {ev.Seq} {ev.Status}");
}

// Client streaming: CompleteAsync — это CloseSend, ответ — свойство-Task.
// Go: Send..., затем CloseAndRecv().
using (var up = client.UploadItems())
{
    await up.RequestStream.WriteAsync(new UploadItemsRequest { Sku = "tea", Quantity = 2, PriceCents = 350 });
    await up.RequestStream.CompleteAsync();
    var sum = await up;
    Console.WriteLine($"загружено: {sum.Count} {sum.TotalCents}");
}

// Отмена посреди стрима: CancellationToken вместо context.WithCancel.
// Читающий получает RpcException со StatusCode.Cancelled
// (Go: status.Code(err) == codes.Canceled).
using var cts = new CancellationTokenSource();
using var follow = client.WatchOrders(new WatchOrdersRequest { Follow = true }, cancellationToken: cts.Token);
try
{
    var n = 0;
    await foreach (var _ in follow.ResponseStream.ReadAllAsync(cts.Token))
        if (++n == 3) cts.Cancel();
}
catch (RpcException ex) when (ex.StatusCode == StatusCode.Cancelled)
{
    Console.WriteLine("клиент: Cancelled");
}

await app.StopAsync();

public class OrderStreamImpl : OrderStreamService.OrderStreamServiceBase
{
    // IServerStreamWriter.WriteAsync ↔ stream.Send; context.CancellationToken ↔
    // stream.Context().Done(). Возврат из метода завершает стрим, как в Go.
    public override async Task WatchOrders(WatchOrdersRequest request,
        IServerStreamWriter<WatchOrdersResponse> responseStream, ServerCallContext context)
    {
        string[] statuses = ["created", "paid", "shipped"];
        for (long i = 1; request.Follow || i <= 3; i++)
        {
            context.CancellationToken.ThrowIfCancellationRequested();
            // WriteAsync ждёт, пока HTTP/2 flow control разрешит отправку, —
            // та же обратная связь, что у блокирующего Send в Go.
            await responseStream.WriteAsync(new WatchOrdersResponse
            {
                Seq = i, OrderId = "ord-1", Status = statuses[(i - 1) % 3],
            });
        }
    }

    // ReadAllAsync заканчивается, когда клиент вызвал CompleteAsync, —
    // в Go это io.EOF из Recv.
    public override async Task<UploadItemsResponse> UploadItems(
        IAsyncStreamReader<UploadItemsRequest> requestStream, ServerCallContext context)
    {
        var resp = new UploadItemsResponse();
        await foreach (var it in requestStream.ReadAllAsync(context.CancellationToken))
        {
            resp.Count++;
            resp.TotalCents += it.Quantity * it.PriceCents;
        }
        return resp;
    }

    // Bidi: оба потока в одном методе. Писать в responseStream из нескольких
    // задач одновременно нельзя — как и Send из нескольких горутин в Go.
    public override async Task TrackOrders(IAsyncStreamReader<TrackOrdersRequest> requestStream,
        IServerStreamWriter<TrackOrdersResponse> responseStream, ServerCallContext context)
    {
        await foreach (var req in requestStream.ReadAllAsync(context.CancellationToken))
            await responseStream.WriteAsync(new TrackOrdersResponse { OrderId = req.OrderId, Status = "unknown" });
    }
}
